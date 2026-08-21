package gitconnect

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/localgit"
)

var (
	// ErrRepositoryNotAccessible indicates the repository is real as far as the
	// caller named it, but the Volcano GitHub App cannot see it through any of
	// their installations.
	ErrRepositoryNotAccessible = errors.New("repository is not accessible through your GitHub connection")
	// ErrBindingChanged indicates the project's binding was not what the command
	// read before it asked the user about it.
	ErrBindingChanged = errors.New("the project's repository connection changed while this command was running")
)

// Target is a resolved repository, and the connection and installation it was
// reached through. It is everything the bind call needs.
//
// ConnectionLogin and InstallationAccount are carried for reporting only. The
// connection decides whose stored GitHub token the platform reads the
// repository with on every future deploy, and more than one connection can
// reach the same repository, so which one was picked is not something to leave
// unsaid.
type Target struct {
	ConnectionID        uuid.UUID
	ConnectionLogin     string
	InstallationID      int64
	InstallationAccount string
	Repository          apiclient.GitRepository
}

// Resolve finds the repository named by a local remote among the repos the
// caller's GitHub connection can reach, so the caller can confirm the binding
// before it is made.
func (s Service) Resolve(ctx context.Context, repository localgit.Repository) (*Target, error) {
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

	// The schema allows one GitHub connection per user, so this loop runs once
	// today. It is written as a walk anyway because the API returns a list, and
	// a failing lookup must not hide a repository a later entry could see.
	// Keep the first failure and report it only if nothing resolves.
	var failure error
	for _, connection := range usable {
		target, err := s.resolveThroughConnection(ctx, authenticated.API, connection, repository)
		switch {
		case target != nil:
			return target, nil
		case err != nil && failure == nil:
			failure = err
		}
	}
	if failure != nil {
		return nil, failure
	}
	return nil, fmt.Errorf("%w: %s", ErrRepositoryNotAccessible, repository.FullName())
}

// resolveThroughConnection looks for the repository among one connection's
// installations. The installation whose account owns the repository is tried
// first; a miss there falls through to the rest, because an installation
// scoped to selected repositories can carry a repo its account does not own.
// A nil target with a nil error means "not found here, keep looking".
func (s Service) resolveThroughConnection(
	ctx context.Context,
	client *api.Client,
	connection apiclient.GitConnection,
	repository localgit.Repository,
) (*Target, error) {
	installations, err := client.ListGitInstallations(ctx, connection.Id)
	if err != nil {
		return nil, classifyProvider(err, "failed to list your GitHub App installations")
	}

	// One installation that cannot be listed must not hide a repository another
	// one holds, for the same reason the connection loop keeps going: the
	// owner's installation is tried first and is exactly the one most likely to
	// be scoped away from the repository.
	var failure error
	for _, installation := range orderByOwner(installations, repository.Owner) {
		repositories, err := client.ListGitInstallationRepositories(ctx, connection.Id, installation.Id)
		if err != nil {
			if failure == nil {
				failure = classifyProvider(err, "failed to list repositories for "+installation.AccountLogin)
			}
			continue
		}

		for _, candidate := range repositories {
			if strings.EqualFold(candidate.FullName, repository.FullName()) {
				return &Target{
					ConnectionID:        connection.Id,
					ConnectionLogin:     connection.ProviderLogin,
					InstallationID:      installation.Id,
					InstallationAccount: installation.AccountLogin,
					Repository:          candidate,
				}, nil
			}
		}
	}
	return nil, failure
}

// Connect binds the current project to a resolved repository, provided its
// binding is still expected — nil for a project that had none.
//
// The bind is a full replace and names no prior state, so it overwrites whatever
// is bound when it arrives. Everything the caller decided rests on a read taken
// before resolving the repository and before any prompt, so the window is wide:
// three provider round-trips plus however long the user takes to answer. Within
// it another actor can point the project somewhere the user was never shown.
// Re-reading narrows that; the API has no conditional write to close it.
func (s Service) Connect(
	ctx context.Context, target Target, rootDirectory string, expected *apiclient.ProjectGitConnection,
) (*apiclient.ProjectGitConnection, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	if err := s.confirmUnchanged(ctx, expected); err != nil {
		return nil, err
	}

	repositoryID := target.Repository.Id
	body := apiclient.ConnectProjectGitJSONRequestBody{
		ConnectionId:   target.ConnectionID,
		InstallationId: target.InstallationID,
		RepositoryId:   &repositoryID,
	}
	if trimmed := strings.TrimSpace(rootDirectory); trimmed != "" {
		body.RootDirectory = &trimmed
	}

	connection, err := authenticated.API.ConnectProjectGit(ctx, authenticated.ProjectID, body)
	if err != nil {
		return nil, classifyProvider(err, "failed to connect "+target.Repository.FullName)
	}
	return connection, nil
}

// confirmUnchanged rejects a write whose premise no longer holds: the binding
// the caller read, showed, and asked about is not the one there now. Both writes
// behind this are full replaces that name no prior state, so without it either
// would silently discard a change made while the command was running.
func (s Service) confirmUnchanged(ctx context.Context, expected *apiclient.ProjectGitConnection) error {
	current, err := s.binding(ctx)
	if err != nil {
		return err
	}
	if sameBinding(current, expected) {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrBindingChanged, describeBinding(current))
}

// binding reads the project's connection, reporting a project with none as a nil
// connection rather than an error, so absence can be compared like any value.
func (s Service) binding(ctx context.Context) (*apiclient.ProjectGitConnection, error) {
	connection, err := s.Status(ctx)
	if errors.Is(err, ErrNotConnected) {
		return nil, nil //nolint:nilnil // no binding is a value here, not a failure
	}
	return connection, err
}

// sameBinding reports whether two reads describe the same binding. Absence
// counts: a project that gained or lost one in between changed.
//
// UpdatedAt does the work the fields cannot: the platform bumps it only when the
// row really changes (migration 033 guards the trigger on NEW IS DISTINCT FROM
// OLD), so it catches mutations to fields this CLI does not model. It is broader
// than the binding alone, so an unrelated change to the project can abort a
// command; that fails closed, and a re-run is cheaper than silently reverting
// someone else's edit.
//
// Every field is compared as well, so this does not depend on a trigger in
// another repository staying the way it is.
func sameBinding(a, b *apiclient.ProjectGitConnection) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.UpdatedAt.Equal(b.UpdatedAt) &&
		a.RepoId == b.RepoId &&
		a.RepoInstallationId == b.RepoInstallationId &&
		a.RootDirectory == b.RootDirectory &&
		a.ProductionBranch == b.ProductionBranch &&
		strings.EqualFold(a.RepoFullName, b.RepoFullName)
}

func describeBinding(connection *apiclient.ProjectGitConnection) string {
	if connection == nil {
		return "it now has no repository connected"
	}
	return "it now points at " + connection.RepoFullName
}

// orderByOwner puts the installation for owner first, leaving the rest in their
// original order behind it.
func orderByOwner(installations []apiclient.GitInstallation, owner string) []apiclient.GitInstallation {
	ordered := make([]apiclient.GitInstallation, 0, len(installations))
	for _, installation := range installations {
		if strings.EqualFold(installation.AccountLogin, owner) {
			ordered = append(ordered, installation)
		}
	}
	for _, installation := range installations {
		if !strings.EqualFold(installation.AccountLogin, owner) {
			ordered = append(ordered, installation)
		}
	}
	return ordered
}
