// Package lavaflow performs the LavaFlow workflows behind the volcano cloud
// lavaflow subcommands. LavaFlow reads a project's runtime errors, diagnoses
// the issues they group into, and opens pull requests that fix them.
package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// OnDescription says what LavaFlow does, and that it uses AI to do it, in
// the wording approved for that disclosure. Keep it verbatim: it opens the
// help, enable's output and the docs.
const OnDescription = "LavaFlow uses AI to diagnose this project's errors from their logs and deployed source, " +
	"and opens fix pull requests in your connected repository."

// ErrUnavailable reports an API without LavaFlow: a platform release older
// than LavaFlow, which has none of its routes. Said in place of the 404 it
// answers, which otherwise reads as a missing project or run.
var ErrUnavailable = errors.New("LavaFlow isn't available on this Volcano API")

// ErrNotAvailableToAccount reports LavaFlow off for this project: the
// project's capabilities say so before a change, or a route answers
// feature_unavailable, while LavaFlow is open only to some accounts. A project
// that already has LavaFlow records keeps reading them, and can turn LavaFlow
// off, while it is unavailable.
var ErrNotAvailableToAccount = errors.New("LavaFlow isn't available to your account yet")

// ErrPersonDecides refuses changing LavaFlow's settings with a project access
// token. Turning LavaFlow on, off or changing how far it goes is a person's
// decision, so the API takes it from a person's session only, and the CLI
// says so before asking.
var ErrPersonDecides = errors.New("changing LavaFlow's settings needs a person's session: a project access token can't")

// ErrOwnerDecides refuses changing LavaFlow's settings for a person who isn't
// the project's owner. The settings say so before the CLI asks to change them.
var ErrOwnerDecides = errors.New("only the project's owner can change LavaFlow's settings, from their own session")

// capabilityID is LavaFlow's entry in a project's capabilities.
const capabilityID = "lavaflow"

// Codes of the API's refusals.
const (
	codeFeatureUnavailable = "feature_unavailable"
	codePersonRequired     = "person_required"
	codeOwnerRequired      = "owner_required"
)

// Limits the API sets.
const (
	MaxRunsLimit  = 100
	DefaultLimit  = 25
	DefaultDays   = 30
	MaxDays       = 90
	MaxTextLength = 4000
)

// Whether a repository is connected for LavaFlow's fix pull requests.
const (
	RepositoryConnected    = "connected"
	RepositoryNotConnected = "not_connected"
	// RepositoryUnknown means the connection could not be read, so the status
	// says nothing about it rather than claim none is connected.
	RepositoryUnknown = "unknown"
)

// Status is a project's LavaFlow settings and whether a repository is
// connected for its fix pull requests.
type Status struct {
	Settings *apiclient.LavaflowSettings `json:"settings"`
	// Available is whether LavaFlow can be turned on or changed for the
	// project, from its capabilities; omitted when they could not be read.
	// While it is false, a project's records stay readable and LavaFlow can
	// still be turned off.
	Available       *bool  `json:"available,omitempty"`
	RepositoryState string `json:"repository_state"`
	// Repository is the connected repository's full name, set only when
	// RepositoryState is RepositoryConnected.
	Repository string `json:"repository,omitempty"`
}

// RunsQuery picks a page of runs. Zero values leave a filter out.
type RunsQuery struct {
	Limit      int
	Cursor     string
	Status     string
	ResourceID string
}

// Service performs LavaFlow workflows against the current project.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a LavaFlow service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// project returns the current project's session. Reads don't ask the
// project's capabilities first: a project with LavaFlow records reads them
// while LavaFlow is unavailable, and a route answers feature_unavailable for
// a project without any.
func (s Service) project() (*clisession.ProjectSession, error) {
	return s.sessions.CurrentProject()
}

func requireAvailable(ctx context.Context, authenticated *clisession.ProjectSession) error {
	capabilities, err := authenticated.API.ListProjectCapabilities(ctx, authenticated.ProjectID)
	if err != nil {
		return unavailable(fmt.Errorf("failed to read the project's capabilities: %w", err))
	}
	if !availableIn(capabilities) {
		return api.WithCause(ErrNotAvailableToAccount,
			&api.Error{StatusCode: http.StatusNotFound, Message: ErrNotAvailableToAccount.Error(), Code: codeFeatureUnavailable})
	}
	return nil
}

// availableIn reports LavaFlow's capability entry as available. A missing
// entry or any other status is unavailable.
func availableIn(capabilities *apiclient.CapabilityList) bool {
	return slices.ContainsFunc(capabilities.Data, func(c apiclient.Capability) bool {
		return c.Id == capabilityID && c.Status == apiclient.CapabilityStatusAvailable
	})
}

// Status returns the current project's LavaFlow settings and its repository
// connection.
func (s Service) Status(ctx context.Context) (*Status, error) {
	authenticated, err := s.project()
	if err != nil {
		return nil, err
	}
	settings, err := authenticated.API.GetLavaflowSettings(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to get LavaFlow settings: %w", err))
	}
	status := &Status{Settings: settings}
	if status.Available, err = availability(ctx, authenticated); err != nil {
		return nil, err
	}
	status.RepositoryState, status.Repository, err = repository(ctx, authenticated)
	if err != nil {
		return nil, err
	}
	return status, nil
}

// availability reads whether LavaFlow takes changes for the project. It is
// detail the status adds, so a capabilities read that fails leaves it unknown;
// a cancel or deadline still ends the command.
func availability(ctx context.Context, authenticated *clisession.ProjectSession) (*bool, error) {
	capabilities, err := authenticated.API.ListProjectCapabilities(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, ctx.Err()
	}
	available := availableIn(capabilities)
	return &available, nil
}

func repository(ctx context.Context, authenticated *clisession.ProjectSession) (state, name string, err error) {
	connection, err := authenticated.API.GetProjectGitConnection(ctx, authenticated.ProjectID)
	switch {
	case err == nil:
		return RepositoryConnected, connection.RepoFullName, nil
	case ctx.Err() != nil:
		// The repository is optional detail, but a cancel or deadline still
		// ends the command.
		return "", "", ctx.Err()
	case api.Status(err) == http.StatusNotFound:
		// The settings read before it proved the project exists, so a 404
		// means no repository; anything else leaves the connection unknown.
		return RepositoryNotConnected, "", nil
	default:
		return RepositoryUnknown, "", nil
	}
}

// EnableNotice is what enable says before it turns LavaFlow on, whether or
// not it then asks, in the wording approved for it. Keep it verbatim.
const EnableNotice = "LavaFlow sends this resource's logs and source code to an AI service that Volcano operates."

// Enable turns LavaFlow on for the current project, sending enabled alone so
// the other settings keep their values. confirm runs once the checks that
// need no answer have passed and before anything changes; an error from it
// stops the change and is returned as is.
func (s Service) Enable(ctx context.Context, confirm func() error) (*apiclient.LavaflowSettings, error) {
	enabled := true
	return s.update(ctx, "turn LavaFlow on", apiclient.LavaflowSettingsUpdate{Enabled: &enabled}, true, confirm)
}

// Disable turns LavaFlow off for the current project, sending enabled alone
// so the other settings keep their values. It works while LavaFlow is
// unavailable for a project that has LavaFlow records.
func (s Service) Disable(ctx context.Context) (*apiclient.LavaflowSettings, error) {
	enabled := false
	return s.update(ctx, "turn LavaFlow off", apiclient.LavaflowSettingsUpdate{Enabled: &enabled}, false, nil)
}

// UpdateSettings changes the fields update sets and keeps the rest.
func (s Service) UpdateSettings(ctx context.Context, update apiclient.LavaflowSettingsUpdate) (*apiclient.LavaflowSettings, error) {
	if update.Autonomy != nil && !update.Autonomy.Valid() {
		return nil, fmt.Errorf("invalid autonomy %q: use %s or %s", *update.Autonomy, apiclient.DiagnoseOnly, apiclient.OpenPr)
	}
	for name, text := range map[string]*string{"custom context": update.CustomContext, "system prompt": update.SystemPrompt} {
		if text != nil && len([]rune(*text)) > MaxTextLength {
			return nil, fmt.Errorf("the %s is %d characters long: the most is %d", name, len([]rune(*text)), MaxTextLength)
		}
	}
	return s.update(ctx, "change LavaFlow's settings", update, true, nil)
}

// update refuses a project access token before sending anything. A change
// that needs LavaFlow available checks the project's capabilities, and every
// change reads can_update before it asks or sends, so a person who isn't the
// owner is told before the change is refused. A non-nil confirm runs just
// before the change.
func (s Service) update(ctx context.Context, verb string, update apiclient.LavaflowSettingsUpdate,
	needsAvailable bool, confirm func() error,
) (*apiclient.LavaflowSettings, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	if config.IsProjectToken(authenticated.Config.Token()) {
		return nil, personDecides(authenticated.Config, config.ErrAccountTokenRequired)
	}
	if needsAvailable {
		if err := requireAvailable(ctx, authenticated); err != nil {
			return nil, err
		}
	}
	current, err := authenticated.API.GetLavaflowSettings(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to get LavaFlow settings: %w", err))
	}
	if !current.CanUpdate {
		return nil, api.WithCause(ErrOwnerDecides,
			&api.Error{StatusCode: http.StatusForbidden, Message: ErrOwnerDecides.Error(), Code: codeOwnerRequired})
	}
	if confirm != nil {
		if err := confirm(); err != nil {
			return nil, err
		}
	}
	settings, err := authenticated.API.UpdateLavaflowSettings(ctx, authenticated.ProjectID, update)
	if err != nil {
		return nil, refused(authenticated.Config, fmt.Errorf("failed to %s: %w", verb, err))
	}
	return settings, nil
}

// refused says why the API refused a change, by its code rather than its
// status, keeping the refusal as the cause for the exit code.
func refused(cfg *config.Config, err error) error {
	switch api.Code(err) {
	case codePersonRequired:
		return personDecides(cfg, err)
	case codeOwnerRequired:
		return api.WithCause(ErrOwnerDecides, err)
	}
	return unavailable(err)
}

// personDecides says how a person changes the settings instead. A token from
// the environment outlasts a sign-in, so that case names the variable to
// unset first. cause is the refusal, the API's or the CLI's own, which keeps
// the exit code of a permission refused.
func personDecides(cfg *config.Config, cause error) error {
	how := "Sign in with 'volcano login' and run this again"
	if cfg.TokenFromEnv() {
		how = "Unset " + config.TokenEnv + " and sign in with 'volcano login', then run this again"
	}
	return api.WithCause(fmt.Errorf("%w. %s", ErrPersonDecides, how), cause)
}

// Stats returns the issues and resolutions LavaFlow opened per day over the
// last days days, today included.
func (s Service) Stats(ctx context.Context, days int) (*apiclient.LavaflowStats, error) {
	if err := checkDays(days); err != nil {
		return nil, err
	}
	authenticated, err := s.project()
	if err != nil {
		return nil, err
	}
	stats, err := authenticated.API.GetLavaflowStats(ctx, authenticated.ProjectID, days)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to get LavaFlow stats: %w", err))
	}
	return stats, nil
}

// Resources returns every function, frontend and durable function in the
// current project with what LavaFlow did for it over the last days days.
func (s Service) Resources(ctx context.Context, days int) (*apiclient.LavaflowResourceList, error) {
	if err := checkDays(days); err != nil {
		return nil, err
	}
	authenticated, err := s.project()
	if err != nil {
		return nil, err
	}
	resources, err := authenticated.API.ListLavaflowResources(ctx, authenticated.ProjectID, days)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to list LavaFlow resources: %w", err))
	}
	return resources, nil
}

func checkDays(days int) error {
	if days < 1 || days > MaxDays {
		return fmt.Errorf("--days must be between 1 and %d", MaxDays)
	}
	return nil
}

// ListRuns returns one page of the current project's runs, newest first.
func (s Service) ListRuns(ctx context.Context, query RunsQuery) (*apiclient.LavaflowRunList, error) {
	if query.Limit < 1 || query.Limit > MaxRunsLimit {
		return nil, fmt.Errorf("--limit must be between 1 and %d", MaxRunsLimit)
	}
	params := apiclient.ListLavaflowRunsParams{Limit: &query.Limit}
	if query.Status != "" {
		status := apiclient.LavaflowRunStatus(query.Status)
		if !status.Valid() {
			return nil, fmt.Errorf("invalid --status %q: use one of %s", query.Status, runStatuses())
		}
		params.Status = &status
	}
	if query.Cursor != "" {
		params.Cursor = &query.Cursor
	}
	if query.ResourceID != "" {
		params.ResourceId = &query.ResourceID
	}
	authenticated, err := s.project()
	if err != nil {
		return nil, err
	}
	page, err := authenticated.API.ListLavaflowRuns(ctx, authenticated.ProjectID, params)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to list LavaFlow runs: %w", err))
	}
	return page, nil
}

func runStatuses() string {
	statuses := []apiclient.LavaflowRunStatus{
		apiclient.LavaflowRunStatusQueued, apiclient.LavaflowRunStatusDiagnosing, apiclient.LavaflowRunStatusFixing,
		apiclient.LavaflowRunStatusReviewing, apiclient.LavaflowRunStatusPrOpen, apiclient.LavaflowRunStatusCompleted,
		apiclient.LavaflowRunStatusFailed, apiclient.LavaflowRunStatusCancelled,
	}
	names := make([]string, 0, len(statuses))
	for _, status := range statuses {
		names = append(names, string(status))
	}
	return strings.Join(names, ", ")
}

// GetRun returns one of the current project's runs with its diagnosis, diff
// and review.
func (s Service) GetRun(ctx context.Context, runID string) (*apiclient.LavaflowRun, error) {
	authenticated, err := s.project()
	if err != nil {
		return nil, err
	}
	run, err := authenticated.API.GetLavaflowRun(ctx, authenticated.ProjectID, runID)
	if err != nil {
		return nil, unavailable(fmt.Errorf("failed to get LavaFlow run %s: %w", runID, err))
	}
	return run, nil
}

// unavailable replaces a refusal that says LavaFlow is not on with the error
// that says why, keeping the refusal so the exit code still reports it.
func unavailable(err error) error {
	switch {
	case api.Code(err) == codeFeatureUnavailable:
		return api.WithCause(ErrNotAvailableToAccount, err)
	case api.RouteMissing(err):
		return api.WithCause(ErrUnavailable, err)
	}
	return err
}
