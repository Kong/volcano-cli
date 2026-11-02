// Package gitconnect binds the current project to a GitHub repository.
//
// The CLI never creates push credentials and never pushes: it resolves a local
// remote to a repository the caller's GitHub connection can already see, and
// asks the platform to bind it. Pushing stays the user's own `git push`, except
// for a source export, where the platform pushes the first commit to an empty
// repository.
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
	"github.com/Kong/volcano-cli/internal/localgit"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

var (
	// ErrNoGitHubConnection indicates the caller has no usable GitHub
	// connection. The CLI cannot create one: the connect flow is a browser
	// redirect bound to an HttpOnly cookie, which no API route starts yet.
	ErrNoGitHubConnection = errors.New("no GitHub account is connected to your Volcano account")
	// ErrRepositoryNotAccessible indicates the repository is real as far as the
	// local remote is concerned, but the Volcano GitHub App cannot see it
	// through any of the caller's installations.
	ErrRepositoryNotAccessible = errors.New("repository is not accessible through your GitHub connection")
	// ErrProviderNotConfigured indicates the API has no GitHub App configured.
	// Local mode is the usual cause: those settings are first-party only.
	ErrProviderNotConfigured = errors.New("git provider integration is not configured on this API")
	// ErrNotConnected indicates the project has no repository bound.
	ErrNotConnected = errors.New("this project is not connected to a repository")
	// ErrBindingChanged indicates the project's binding was not what the command
	// read before it asked the user about it.
	ErrBindingChanged = errors.New("the project's repository connection changed while this command was running")
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

// accountScoped returns the pinned session for a call to the caller's own
// GitHub connections, which hang off the account rather than the project. The
// platform does not admit a project access token there, and its 403 would be
// read as the project being wrong — so the missing credential is named here
// instead.
//
// This checks the pinned session rather than asking the factory to resolve an
// account-scoped one, which would resolve a second project: the whole point of
// the pin is that every call in a command acts on the same one.
func (s Service) accountScoped() (*clisession.ProjectSession, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}
	if err := authenticated.Config.RequireAccountToken(); err != nil {
		return nil, err
	}
	return authenticated, nil
}

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
	if current := authenticated.Config.Project(); current != nil && current.ID == ref.ID.String() {
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
			return api.WithCause(fmt.Errorf("%w: %s", ErrProjectNotFound, authenticated.ProjectID), err)
		}
		// The project could not be confirmed either way, which is not the same
		// as knowing it has no binding, so the failure is reported as itself.
		return classify(err, "failed to confirm the selected project exists")
	}
	return ErrNotConnected
}

// Resolve finds the repository named by a local remote among the repos the
// caller's GitHub connection can reach, so the caller can confirm the binding
// before it is made.
func (s Service) Resolve(ctx context.Context, repository localgit.Repository) (*Target, error) {
	authenticated, err := s.accountScoped()
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
// binding is still expected — nil for a project that had none. A non-empty
// productionBranch is the branch it deploys from; empty follows the
// repository's default, or keeps a branch pinned on the same repository.
//
// The bind is a full replace and names no prior state, so it overwrites whatever
// is bound when it arrives. Everything the caller decided rests on a read taken
// before resolving the repository and before any prompt, so the window is wide:
// three provider round-trips plus however long the user takes to answer. Within
// it another actor can point the project somewhere the user was never shown.
// Re-reading narrows that; the API has no conditional write to close it.
func (s Service) Connect(
	ctx context.Context, target Target, rootDirectory, productionBranch string, expected *apiclient.ProjectGitConnection,
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
	// The bind is a full replace, and leaving the branch out follows the
	// repository's default, which would discard a branch the project pinned.
	// Only for the same repository, though: a pin was chosen for the
	// repository it belongs to, and naming it while changing repository is
	// refused.
	switch {
	case productionBranch != "":
		body.ProductionBranch = &productionBranch
	case expected != nil && expected.RepoId == repositoryID && !expected.ProductionBranchFollowsDefault:
		branch := expected.ProductionBranch
		body.ProductionBranch = &branch
	}

	connection, err := authenticated.API.ConnectProjectGit(ctx, authenticated.ProjectID, body)
	if err != nil {
		return nil, classifyProvider(err, "failed to connect "+target.Repository.FullName)
	}
	return connection, nil
}

// SetRootDirectory changes only the directory the project builds from, leaving
// the repository bound alone. It is the one change to a binding a project
// access token may make, so CI and agents can move a project within the
// repository a person chose.
//
// When expected is not nil, the binding must still be the one it describes, as
// for Connect: a command that read the binding before resolving the repository
// must not overwrite a root directory someone changed in the meantime. A nil
// expected skips that read, for a caller that read nothing first.
func (s Service) SetRootDirectory(
	ctx context.Context, rootDirectory string, expected *apiclient.ProjectGitConnection,
) (*apiclient.ProjectGitConnection, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	if expected != nil {
		if err := s.confirmUnchanged(ctx, expected); err != nil {
			return nil, err
		}
	}

	connection, err := authenticated.API.SetProjectGitRootDirectory(ctx, authenticated.ProjectID, rootDirectory)
	if err != nil {
		// The route answers 404 both for a project with no repository and for
		// a project that does not exist, like the binding read.
		if api.Status(err) == http.StatusNotFound {
			return nil, s.explainNotFound(ctx)
		}
		return nil, classify(err, "failed to set the root directory")
	}
	return connection, nil
}

// CanConnect reports ErrProviderNotConfigured when this API has no GitHub App,
// so no repository can be connected through it at all. That is the case in
// local mode, and it is a better answer than "connect one" for a project with
// nothing connected. It answers nil whenever it cannot tell, including for a
// project access token, which may not read the account's GitHub connections.
func (s Service) CanConnect(ctx context.Context) error {
	authenticated, err := s.current()
	if err != nil || authenticated.Config.RequireAccountToken() != nil {
		return nil //nolint:nilerr // unknown is not a refusal
	}
	if _, err := authenticated.API.ListGitConnections(ctx); api.Status(err) == http.StatusServiceUnavailable {
		return ErrProviderNotConfigured
	}
	return nil
}

// Disconnect removes the project's repository binding, provided it is still the
// one in expected. The repository itself is untouched.
//
// The delete names no repository, so it removes whatever is bound when it
// arrives. Re-reading first keeps the command from deleting a binding the user
// was never shown — the window between reading a binding and confirming it is
// as long as the user takes to answer. It narrows that window rather than
// closing it; the API has no conditional delete to close it with.
func (s Service) Disconnect(ctx context.Context, expected *apiclient.ProjectGitConnection) error {
	authenticated, err := s.current()
	if err != nil {
		return err
	}

	if err := s.confirmUnchanged(ctx, expected); err != nil {
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
		a.ProductionBranchFollowsDefault == b.ProductionBranchFollowsDefault &&
		strings.EqualFold(a.RepoFullName, b.RepoFullName)
}

func describeBinding(connection *apiclient.ProjectGitConnection) string {
	if connection == nil {
		return "it now has no repository connected"
	}
	return "it now points at " + connection.RepoFullName
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

// SetFrontend selects the frontend a push to the production branch deploys,
// and the directory it builds from relative to the project's root directory.
// An empty name deploys no frontend, and an empty appRoot builds from the root
// directory itself.
func (s Service) SetFrontend(ctx context.Context, name, appRoot string) (*apiclient.ProjectGitDeploySettings, error) {
	return s.changeDeploySettings(ctx, "failed to set the frontend",
		func(settings *apiclient.UpdateProjectGitDeploySettingsRequest) {
			settings.FrontendName, settings.FrontendAppRoot = nil, nil
			if name != "" {
				settings.FrontendName = &name
				if appRoot != "" {
					settings.FrontendAppRoot = &appRoot
				}
			}
		})
}

// SetAutoDeploy turns deploying on a push to the production branch on or off.
func (s Service) SetAutoDeploy(ctx context.Context, on bool) (*apiclient.ProjectGitDeploySettings, error) {
	return s.changeDeploySettings(ctx, "failed to turn auto-deploy "+onOff(on),
		func(settings *apiclient.UpdateProjectGitDeploySettingsRequest) {
			settings.AutoDeployEnabled = on
		})
}

// SetDeployFunctions turns deploying the repository's functions on a push on
// or off.
func (s Service) SetDeployFunctions(ctx context.Context, on bool) (*apiclient.ProjectGitDeploySettings, error) {
	return s.changeDeploySettings(ctx, "failed to turn functions "+onOff(on),
		func(settings *apiclient.UpdateProjectGitDeploySettingsRequest) {
			settings.DeployFunctions = on
		})
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// changeDeploySettings changes one deploy setting. The settings write is a
// full replace, so the others are read first and sent back as they were.
// Nothing is asked in between, so a change made elsewhere can only be lost in
// the time two requests take.
func (s Service) changeDeploySettings(
	ctx context.Context, action string, change func(*apiclient.UpdateProjectGitDeploySettingsRequest),
) (*apiclient.ProjectGitDeploySettings, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	current, err := authenticated.API.GetProjectGitDeploySettings(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, settingsError(authenticated.ProjectID, err, "failed to read the project's deploy settings")
	}
	request := settingsRequest(current)
	change(&request)
	settings, err := authenticated.API.UpdateProjectGitDeploySettings(ctx, authenticated.ProjectID, request)
	if err != nil {
		return nil, settingsError(authenticated.ProjectID, err, action)
	}
	return settings, nil
}

// settingsRequest is the write that leaves every setting as read. An empty
// frontend name means none, and the platform refuses an app root without one,
// so neither is sent unless a frontend is named.
func settingsRequest(current *apiclient.ProjectGitDeploySettings) apiclient.UpdateProjectGitDeploySettingsRequest {
	request := apiclient.UpdateProjectGitDeploySettingsRequest{
		AutoDeployEnabled: current.AutoDeployEnabled,
		DeployFunctions:   current.DeployFunctions,
	}
	if current.FrontendName != nil && *current.FrontendName != "" {
		request.FrontendName = current.FrontendName
		if current.FrontendAppRoot != nil && *current.FrontendAppRoot != "" {
			request.FrontendAppRoot = current.FrontendAppRoot
		}
	}
	return request
}

// SetProductionBranch changes only the branch a push must land on to deploy.
// A branch pins the project to it, so a default-branch rename on GitHub no
// longer moves it; an empty one makes the project follow the repository's
// default branch again.
func (s Service) SetProductionBranch(ctx context.Context, branch string) (*apiclient.ProjectGitConnection, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	connection, err := authenticated.API.SetProjectGitProductionBranch(ctx, authenticated.ProjectID, branch)
	if err != nil {
		// The route answers 404 both for a project with no repository and for
		// a project that does not exist, like the binding read.
		if api.Status(err) == http.StatusNotFound {
			return nil, s.explainNotFound(ctx)
		}
		return nil, classify(err, "failed to set the production branch")
	}
	return connection, nil
}

// settingsError classifies a deploy-settings failure. The settings exist for
// every project, connected or not, so a 404 on them means the project itself
// is gone. The API error stays the cause, so the exit code still says "not
// found".
func settingsError(projectID uuid.UUID, err error, action string) error {
	if api.Status(err) == http.StatusNotFound {
		return api.WithCause(fmt.Errorf("%w: %s", ErrProjectNotFound, projectID), err)
	}
	return classify(err, action)
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
	switch api.Status(err) {
	case http.StatusServiceUnavailable:
		return ErrProviderNotConfigured
	case http.StatusUnauthorized:
		// Authentication, per the contract — see ErrNotAuthenticated. Reporting
		// this as a GitHub reconnect sent users after a failure only signing in
		// again can fix.
		return fmt.Errorf("%w: %w", ErrNotAuthenticated, err)
	default:
		return classify(err, action)
	}
}

// classify annotates an error with what was being done when it happened.
func classify(err error, action string) error {
	return fmt.Errorf("%s: %w", action, err)
}
