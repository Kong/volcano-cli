package gitconnect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

var (
	// ErrOwnerNotInstallable indicates the caller named a GitHub account the
	// Volcano App is not installed on. Creating there would fail at the provider,
	// and the accounts that would work are known, so this is refused with them
	// named rather than sent.
	ErrOwnerNotInstallable = errors.New("the Volcano GitHub App is not installed on that account")
	// ErrCreateNotFound indicates the platform could not find something the create
	// needs: the project, a connected GitHub account, or an App installation on
	// the owner. All three answer 404 and only the platform's message tells them
	// apart, so it is kept as the text the user reads — this exists to carry the
	// dashboard link, which is where two of the three are fixed.
	//
	// It names no repository on purpose: a 404 is refused before the provider is
	// reached, so nothing was created.
	ErrCreateNotFound = errors.New("failed to create the repository")
	// ErrRepositoryMayExist indicates a creation whose failure does not mean
	// nothing was created. The repository is made on GitHub before the binding is
	// written and before some of the failures below can happen, so a caller that
	// reads these as "nothing happened" and retries with a new name ends up
	// owning two repositories.
	ErrRepositoryMayExist = errors.New("a repository may have been created on GitHub")
)

// CreateRepositoryInput describes the repository to create for the current
// project.
type CreateRepositoryInput struct {
	Name string
	// Owner is the GitHub account to create under. Empty means the connected
	// account, which the platform resolves — the CLI does not substitute a login
	// for it, because "the account this connection belongs to" is the platform's
	// question to answer and sending a name would make it a different request.
	Owner       string
	Private     bool
	Description string
	// RootDirectory is the subdirectory the project builds from, empty for the
	// repository root.
	RootDirectory string
	// ProductionBranch is the branch that deploys. Empty leaves the platform to
	// predict it from the account's default branch name, which is what the CLI
	// avoids by passing the branch it is about to push.
	ProductionBranch string
}

// CreateRepository creates a new, empty GitHub repository and binds the current
// project to it.
//
// This is the only call in this package that creates something outside Volcano,
// and nothing can undo it: there is no delete. So every failure reports whether
// a repository may exist rather than only that the request failed.
//
// Callers are expected to have run CheckOwner first, which is where a named
// account the App cannot create under is refused without sending anything. The
// platform refuses it too, so skipping the check costs a worse message rather
// than a wrong outcome.
func (s Service) CreateRepository(
	ctx context.Context, input CreateRepositoryInput,
) (*apiclient.CreatedProjectGitConnection, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	created, err := authenticated.API.CreateProjectGitRepository(ctx, authenticated.ProjectID, createBody(input))
	if err != nil {
		return nil, classifyCreate(err, input.Name)
	}
	return created, nil
}

func createBody(input CreateRepositoryInput) apiclient.CreateProjectGitRepositoryJSONRequestBody {
	private := input.Private
	body := apiclient.CreateProjectGitRepositoryJSONRequestBody{Name: input.Name, Private: &private}
	if owner := strings.TrimSpace(input.Owner); owner != "" {
		body.Owner = &owner
	}
	if description := strings.TrimSpace(input.Description); description != "" {
		body.Description = &description
	}
	// Sent only when set. An empty root directory means the repository root, which
	// is what omitting the field already means, and a new repository has no
	// previous value for an empty string to have to clear.
	if root := strings.TrimSpace(input.RootDirectory); root != "" {
		body.RootDirectory = &root
	}
	if branch := strings.TrimSpace(input.ProductionBranch); branch != "" {
		body.ProductionBranch = &branch
	}
	return body
}

// CheckCreatable refuses what can be known before anything is created: a caller
// with no GitHub account connected at all, and a named owner the App cannot
// create under.
//
// Both are refused by the platform too, with a 404 that covers three causes and
// cannot say which — nor which accounts would have worked. This can, because it
// has the connection and installation lists in hand, and it does so before the
// user is asked to confirm an irreversible create.
//
// An owner that was not named is deliberately not resolved. "The account this
// connection belongs to" is the platform's question, and guessing a login here
// would let the CLI refuse a create the platform would have accepted.
func (s Service) CheckCreatable(ctx context.Context, owner string) error {
	owner = strings.TrimSpace(owner)
	connections, err := s.gitHubConnections(ctx)
	if err != nil {
		return err
	}
	if owner == "" {
		// A connection exists, which is all an unnamed owner needs: the platform
		// resolves the account from it.
		return nil
	}

	accounts, err := s.installableAccounts(ctx, connections)
	if err != nil {
		return err
	}
	for _, account := range accounts {
		if strings.EqualFold(account, owner) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s%s", ErrOwnerNotInstallable, owner, listAccounts(accounts))
}

// gitHubConnections returns the caller's usable GitHub connections, reporting
// ErrNoGitHubConnection when there are none. That is the commonest reason a
// create cannot work, and the CLI cannot fix it: connecting an account is a
// browser redirect bound to an HttpOnly cookie, so it happens in the dashboard.
func (s Service) gitHubConnections(ctx context.Context) ([]apiclient.GitConnection, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	connections, err := authenticated.API.ListGitConnections(ctx)
	if err != nil {
		return nil, classifyProvider(err, "failed to list your GitHub connections")
	}
	usable := githubConnections(connections)
	if len(usable) == 0 {
		return nil, ErrNoGitHubConnection
	}
	return usable, nil
}

// installableAccounts lists the accounts the given connections have an App
// installation on.
func (s Service) installableAccounts(
	ctx context.Context, connections []apiclient.GitConnection,
) ([]string, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	accounts := make([]string, 0, len(connections))
	for _, connection := range connections {
		installations, err := authenticated.API.ListGitInstallations(ctx, connection.Id)
		if err != nil {
			return nil, classifyProvider(err, "failed to list your GitHub App installations")
		}
		for _, installation := range installations {
			accounts = append(accounts, installation.AccountLogin)
		}
	}
	return accounts, nil
}

func listAccounts(accounts []string) string {
	if len(accounts) == 0 {
		return ". The App is not installed on any account"
	}
	return ". It is installed on: " + strings.Join(accounts, ", ")
}

// noHTTPStatus is what api.Status reports for an error that never became an HTTP
// response: a connection that dropped, a timeout, a cancelled context.
const noHTTPStatus = 0

// classifyCreate maps a creation failure, keeping the platform's own message —
// it is the only thing that can tell three causes of a 404 apart, or say which
// of GitHub's validation refusals happened.
//
// The statuses that may leave a repository behind are marked as such. 409 covers
// both a project that is already connected and a repository that was created and
// landed under another account; 422 covers a retry of a request that already
// created one; 500 covers a creation whose outcome GitHub never reported. None
// of them can be read as "nothing was created" from the status alone.
//
// Neither can no status at all, which is the worst case of the three: the
// request may have reached GitHub and the answer may have been lost on the way
// back, so the repository exists and nothing said so. It also covers a request
// that never left the machine, which created nothing — indistinguishable from
// here, and worth over-reporting: the cost is a glance at the GitHub account,
// against a repository nobody knows about.
func classifyCreate(err error, name string) error {
	switch api.Status(err) {
	case http.StatusUnauthorized:
		return fmt.Errorf("%w: %w", ErrNotAuthenticated, err)
	case http.StatusServiceUnavailable:
		return ErrProviderNotConfigured
	case http.StatusNotFound:
		return fmt.Errorf("%w: %w", ErrCreateNotFound, err)
	case noHTTPStatus, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusInternalServerError:
		return fmt.Errorf("failed to create %s: %w: %w", name, ErrRepositoryMayExist, err)
	default:
		return classify(err, "failed to create "+name)
	}
}
