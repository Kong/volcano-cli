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
	"sync"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

var (
	// ErrNoGitHubConnection indicates the caller has no usable GitHub
	// connection. The CLI cannot create one: the connect flow is a browser
	// redirect bound to an HttpOnly cookie, so it has to happen in the
	// dashboard.
	ErrNoGitHubConnection = errors.New("no GitHub account is connected")
	// ErrProviderNotConfigured indicates the API has no GitHub App configured.
	// Local mode is the usual cause: those settings are first-party only.
	ErrProviderNotConfigured = errors.New("git provider integration is not configured on this API")
	// ErrNotConnected indicates the project has no repository bound.
	ErrNotConnected = errors.New("this project is not connected to a repository")
	// ErrNotAuthenticated indicates the platform rejected this CLI's own
	// credential on a Git route.
	//
	// Named for the CLI session because that is the cause the contract gives:
	// every route this flow calls documents its 401 as authentication — "Not
	// authenticated" on the connection routes, "Unauthorized - invalid or missing
	// token" on the project binding — and none documents a 401 for the stored
	// GitHub token. "The provider connection must be reconnected" is a 409, and
	// only on the import routes, which this flow never calls. So a 401 here is
	// first of all a session to renew, and reconnecting GitHub is the fallback.
	ErrNotAuthenticated = errors.New("not authenticated: your CLI session may have expired")
	// ErrProjectNotFound indicates the selected project does not exist. The
	// binding read answers 404 for this and for a project with no repository
	// connected, so the two are told apart before either is reported.
	ErrProjectNotFound = errors.New("the selected project does not exist")
)

const githubProvider = "github"

// Service performs project git-connection work for the current project.
type Service struct {
	sessions clisession.Factory
	pinned   *pinnedProject
}

// pinnedProject resolves the project once per service. Every call would
// otherwise re-read the configuration, so a command that reads a binding, asks
// the user about it, and then writes could act on three different projects if
// the configuration changed underneath it.
type pinnedProject struct {
	once    sync.Once
	session *clisession.ProjectSession
	err     error
}

// NewService returns a git connection service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps), pinned: &pinnedProject{}}
}

// current returns the project session this service is pinned to.
func (s Service) current() (*clisession.ProjectSession, error) {
	s.pinned.once.Do(func() {
		s.pinned.session, s.pinned.err = s.sessions.CurrentProject()
	})
	return s.pinned.session, s.pinned.err
}

// ProjectRef identifies the project a command acts on. The repository comes
// from the working directory and the project comes from the CLI's own
// configuration, so the two are chosen independently and both have to be
// reported.
type ProjectRef struct {
	ID uuid.UUID
	// Name is empty when the configuration cannot name the selected project,
	// which is what VOLCANO_PROJECT_ID pointing somewhere else looks like.
	Name string
}

// Label renders the project for output: its name where one is known, and always
// enough to identify it.
func (p ProjectRef) Label() string {
	if p.Name == "" {
		return p.ID.String()
	}
	return fmt.Sprintf("%s (%s)", p.Name, p.ID)
}

// Project returns the project these commands act on.
func (s Service) Project() (ProjectRef, error) {
	authenticated, err := s.current()
	if err != nil {
		return ProjectRef{}, err
	}

	ref := ProjectRef{ID: authenticated.ProjectID}
	// Only trust the stored name when it belongs to the selected project: an
	// environment override changes the id without changing the name beside it.
	if current := authenticated.Config.CurrentProject; current != nil && current.ID == ref.ID.String() {
		ref.Name = current.Name
	}
	return ref, nil
}

// Status returns the project's current connection, or ErrNotConnected when it
// has none. A project without a binding answers 404, which is an outcome here
// rather than a failure.
func (s Service) Status(ctx context.Context) (*apiclient.ProjectGitConnection, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	connection, err := authenticated.API.GetProjectGitConnection(ctx, authenticated.ProjectID)
	if err != nil {
		if api.Status(err) == http.StatusNotFound {
			return nil, s.explainNotFound(ctx)
		}
		return nil, classify(err, "failed to get the project's repository connection")
	}
	return connection, nil
}

// explainNotFound resolves the one ambiguous status this API has: the binding
// read answers 404 both for a project with no repository connected and for a
// project that does not exist — a deleted one, or a VOLCANO_PROJECT_ID naming
// nothing. Reporting the benign reading for both would tell a script that an
// invalid selection is a valid unbound project.
func (s Service) explainNotFound(ctx context.Context) error {
	authenticated, err := s.current()
	if err != nil {
		return err
	}

	if _, err := authenticated.API.GetProject(ctx, authenticated.ProjectID); err != nil {
		if api.Status(err) == http.StatusNotFound {
			return fmt.Errorf("%w: %s", ErrProjectNotFound, authenticated.ProjectID)
		}
		// The project could not be confirmed either way, which is not the same
		// as knowing it has no binding, so the failure is reported as itself.
		return classify(err, "failed to confirm the selected project exists")
	}
	return ErrNotConnected
}

// DeploySettings returns what a push to the production branch deploys. It is
// reported alongside a successful connect so the user learns straight away
// whether a push does anything.
func (s Service) DeploySettings(ctx context.Context) (*apiclient.ProjectGitDeploySettings, error) {
	authenticated, err := s.current()
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
	// Resolve the web URL from the API URL the session will actually use, so a
	// runtime override does not produce a link to a different environment.
	webURL := cfg.WebURLForAPIURL(s.sessions.APIURL(cfg))
	return strings.TrimSuffix(webURL, "/") + "/dashboard/project-settings/git", nil
}

// githubConnections keeps the connections for the provider this CLI can bind.
//
// Status is deliberately not filtered on. The column is constrained to
// ('active', 'revoked'), only ever written 'active', and disconnecting deletes
// the row instead of marking it — so a "revoked" filter could never fire, and a
// dead connection announces itself as a 401 on the next call rather than in this
// list. The schema also holds one connection per user and provider, so this
// returns at most one entry today; it stays a list because that is what the API
// returns.
func githubConnections(connections []apiclient.GitConnection) []apiclient.GitConnection {
	usable := make([]apiclient.GitConnection, 0, len(connections))
	for _, connection := range connections {
		if strings.EqualFold(connection.Provider, githubProvider) {
			usable = append(usable, connection)
		}
	}
	return usable
}

// classifyProvider turns a 503 into ErrProviderNotConfigured so the command
// layer can explain it once. Only the routes whose contract defines a 503 use
// this: on a route that does not, a 503 came from something in front of the
// API and saying "no GitHub App configured" would be a guess.
func classifyProvider(err error, action string) error {
	switch api.Status(err) {
	case http.StatusServiceUnavailable:
		return ErrProviderNotConfigured
	case http.StatusUnauthorized:
		// Authentication, per the contract — see ErrNotAuthenticated. Reporting
		// this as a GitHub reconnect sent users to the dashboard for a failure
		// only signing in again can fix.
		return fmt.Errorf("%w: %w", ErrNotAuthenticated, err)
	default:
		return classify(err, action)
	}
}

// classify annotates an error with what was being done when it happened.
func classify(err error, action string) error {
	return fmt.Errorf("%s: %w", action, err)
}
