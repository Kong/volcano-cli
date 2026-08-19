// Package gitconnect binds the current project to a GitHub repository.
//
// The CLI never creates push credentials and never pushes: it resolves a local
// remote to a repository the caller's GitHub connection can already see, and
// asks the platform to bind it. Pushing stays the user's own `git push`.
package gitconnect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/localgit"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

var (
	// ErrNoGitHubConnection indicates the caller has no usable GitHub
	// connection. The CLI cannot create one: the connect flow is a browser
	// redirect bound to an HttpOnly cookie, so it has to happen in the
	// dashboard.
	ErrNoGitHubConnection = errors.New("no GitHub account is connected")
	// ErrRepositoryNotAccessible indicates the repository is real as far as the
	// local remote is concerned, but the Volcano GitHub App cannot see it
	// through any of the caller's installations.
	ErrRepositoryNotAccessible = errors.New("repository is not accessible through your GitHub connection")
	// ErrProviderNotConfigured indicates the API has no GitHub App configured.
	// Local mode is the usual cause: those settings are first-party only.
	ErrProviderNotConfigured = errors.New("git provider integration is not configured on this API")
	// ErrNotConnected indicates the project has no repository bound.
	ErrNotConnected = errors.New("this project is not connected to a repository")
)

const githubProvider = "github"

// Service performs project git-connection work for the current project.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a git connection service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// Target is a resolved repository, and the connection and installation it was
// reached through. It is everything the bind call needs.
type Target struct {
	ConnectionID   uuid.UUID
	InstallationID int64
	Repository     apiclient.GitRepository
}

// Status returns the project's current connection, or ErrNotConnected when it
// has none. A project without a binding answers 404, which is an outcome here
// rather than a failure.
func (s Service) Status(ctx context.Context) (*apiclient.ProjectGitConnection, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	connection, err := authenticated.API.GetProjectGitConnection(ctx, authenticated.ProjectID)
	if err != nil {
		if api.Status(err) == http.StatusNotFound {
			return nil, ErrNotConnected
		}
		return nil, classify(err, "failed to get the project's repository connection")
	}
	return connection, nil
}

// Resolve finds the repository named by a local remote among the repos the
// caller's GitHub connection can reach, so the caller can confirm the binding
// before it is made.
func (s Service) Resolve(ctx context.Context, repository localgit.Repository) (*Target, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	connections, err := authenticated.API.ListGitConnections(ctx)
	if err != nil {
		return nil, classifyProvider(err, "failed to list your GitHub connections")
	}

	usable := usableConnections(connections)
	if len(usable) == 0 {
		return nil, ErrNoGitHubConnection
	}

	// One unhealthy connection must not hide a repository another can see.
	// Connection status is provider-defined free text, so a dead connection is
	// not reliably filtered out beforehand — it shows up as a failing lookup
	// here. Keep the first failure and report it only if nothing resolves.
	var failure error
	for _, connection := range usable {
		target, err := s.resolveThroughConnection(ctx, authenticated.API, connection.Id, repository)
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
	connectionID uuid.UUID,
	repository localgit.Repository,
) (*Target, error) {
	installations, err := client.ListGitInstallations(ctx, connectionID)
	if err != nil {
		return nil, classifyProvider(err, "failed to list your GitHub App installations")
	}

	// One installation that cannot be listed must not hide a repository another
	// one holds, for the same reason the connection loop keeps going: the
	// owner's installation is tried first and is exactly the one most likely to
	// be scoped away from the repository.
	var failure error
	for _, installation := range orderByOwner(installations, repository.Owner) {
		repositories, err := client.ListGitInstallationRepositories(ctx, connectionID, installation.Id)
		if err != nil {
			if failure == nil {
				failure = classifyProvider(err, "failed to list repositories for "+installation.AccountLogin)
			}
			continue
		}

		for _, candidate := range repositories {
			if strings.EqualFold(candidate.FullName, repository.FullName()) {
				return &Target{
					ConnectionID:   connectionID,
					InstallationID: installation.Id,
					Repository:     candidate,
				}, nil
			}
		}
	}
	return nil, failure
}

// Connect binds the current project to a resolved repository.
func (s Service) Connect(ctx context.Context, target Target, rootDirectory string) (*apiclient.ProjectGitConnection, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
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

// Disconnect removes the project's repository binding. The repository itself is
// untouched.
func (s Service) Disconnect(ctx context.Context) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DisconnectProjectGit(ctx, authenticated.ProjectID); err != nil {
		if api.Status(err) == http.StatusNotFound {
			return ErrNotConnected
		}
		return classify(err, "failed to disconnect the repository")
	}
	return nil
}

// DeploySettings returns what a push to the production branch deploys. It is
// reported alongside a successful connect so the user learns straight away
// whether a push does anything.
func (s Service) DeploySettings(ctx context.Context) (*apiclient.ProjectGitDeploySettings, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	settings, err := authenticated.API.GetProjectGitDeploySettings(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, classify(err, "failed to read the project's deploy settings")
	}
	return settings, nil
}

// WebURL returns the dashboard URL for the configured API, so callers can point
// at the page that owns a flow the CLI cannot run itself.
func (s Service) WebURL() (string, error) {
	cfg, err := s.sessions.Config()
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(cfg.WebURL(), "/") + "/dashboard/project-settings/git", nil
}

// usableConnections keeps the GitHub connections that are not flagged as
// needing reconnection. Status is provider-defined text, so anything other than
// an explicit bad state is treated as usable rather than guessed at.
func usableConnections(connections []apiclient.GitConnection) []apiclient.GitConnection {
	usable := make([]apiclient.GitConnection, 0, len(connections))
	for _, connection := range connections {
		if !strings.EqualFold(connection.Provider, githubProvider) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(connection.Status), "revoked") {
			continue
		}
		usable = append(usable, connection)
	}
	return usable
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

// classifyProvider turns a 503 into ErrProviderNotConfigured so the command
// layer can explain it once. Only the routes whose contract defines a 503 use
// this: on a route that does not, a 503 came from something in front of the
// API and saying "no GitHub App configured" would be a guess.
func classifyProvider(err error, action string) error {
	if api.Status(err) == http.StatusServiceUnavailable {
		return fmt.Errorf("%w", ErrProviderNotConfigured)
	}
	return classify(err, action)
}

// classify annotates an error with what was being done when it happened.
func classify(err error, action string) error {
	return fmt.Errorf("%s: %w", action, err)
}
