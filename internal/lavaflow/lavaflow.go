// Package lavaflow performs the LavaFlow workflows behind the volcano cloud
// lavaflow subcommands. LavaFlow reads a project's runtime errors, diagnoses
// the issues they group into, and opens draft pull requests that fix them.
package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// CostNote is what LavaFlow costs during the preview, worded as the dashboard
// and pricing page word it.
const CostNote = "LavaFlow is a usage-billed add-on, priced per issue and per fix pull request. " +
	"Prices are announced before billing starts."

// OnDescription says what turning LavaFlow on does: runs start without a
// request, so the user is told before and after.
const OnDescription = "LavaFlow diagnoses new and reopened issues on its own and opens draft pull " +
	"requests that fix them in the project's connected repository."

// codePlanRequired is the code of a refusal the owner's plan causes.
const codePlanRequired = "plan_required"

// MaxRunsLimit is the largest page of runs the API returns.
const MaxRunsLimit = 100

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
	Settings        *apiclient.LavaFlowSettings `json:"settings"`
	RepositoryState string                      `json:"repository_state"`
	// Repository is the connected repository's full name, set only when
	// RepositoryState is RepositoryConnected.
	Repository string `json:"repository,omitempty"`
}

// Service performs LavaFlow workflows against the current project.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a LavaFlow service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// Status returns the current project's LavaFlow settings and its repository
// connection.
func (s Service) Status(ctx context.Context) (*Status, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	settings, err := authenticated.API.GetLavaFlowSettings(ctx, authenticated.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("failed to get LavaFlow settings: %w", err)
	}
	status := &Status{Settings: settings, RepositoryState: RepositoryUnknown}
	connection, err := authenticated.API.GetProjectGitConnection(ctx, authenticated.ProjectID)
	switch {
	case err == nil:
		status.RepositoryState = RepositoryConnected
		status.Repository = connection.RepoFullName
	case api.Status(err) == http.StatusNotFound:
		// The settings read already proved the project exists, so a 404 means
		// no repository; anything else leaves the connection unknown.
		status.RepositoryState = RepositoryNotConnected
	}
	return status, nil
}

// SetEnabledResult is the saved settings, and whether the stage and trigger
// went back to their defaults because the saved ones could not be read.
type SetEnabledResult struct {
	Settings *apiclient.LavaFlowSettings
	Reset    bool
}

// SetEnabled turns LavaFlow on or off for the current project. The API
// replaces the whole settings object, so the other saved fields are sent back
// unchanged: a manifest's max_stage and auto_trigger survive.
//
// Reading the settings needs the owner's plan, and turning LavaFlow off does
// not, so an outage of the plan service must not keep it on: when the read
// answers 503, disabling saves enabled false alone and reports the reset.
func (s Service) SetEnabled(ctx context.Context, enabled bool) (*SetEnabledResult, error) {
	verb := "turn LavaFlow off"
	if enabled {
		verb = "turn LavaFlow on"
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	update := apiclient.UpdateLavaFlowSettingsRequest{Enabled: enabled}
	current, err := authenticated.API.GetLavaFlowSettings(ctx, authenticated.ProjectID)
	switch {
	case err == nil:
		update.MaxStage = &current.MaxStage
		update.AutoTrigger = &current.AutoTrigger
	case !enabled && api.Status(err) == http.StatusServiceUnavailable:
	default:
		return nil, fmt.Errorf("failed to %s: reading the current settings: %w", verb, err)
	}
	settings, err := authenticated.API.UpdateLavaFlowSettings(ctx, authenticated.ProjectID, update)
	if err != nil {
		return nil, refusal(fmt.Errorf("failed to %s: %w", verb, err))
	}
	return &SetEnabledResult{Settings: settings, Reset: update.MaxStage == nil}, nil
}

// ListRuns returns one page of the current project's runs, newest first.
// cursor is "" for the first page.
func (s Service) ListRuns(ctx context.Context, limit int, cursor string) (*apiclient.PaginatedLavaFlowRuns, error) {
	if limit < 1 || limit > MaxRunsLimit {
		return nil, fmt.Errorf("--limit must be between 1 and %d", MaxRunsLimit)
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	params := apiclient.ListLavaFlowRunsParams{Limit: &limit}
	if cursor != "" {
		params.Cursor = &cursor
	}
	page, err := authenticated.API.ListLavaFlowRuns(ctx, authenticated.ProjectID, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list LavaFlow runs: %w", err)
	}
	return page, nil
}

// GetRun returns one of the current project's runs.
func (s Service) GetRun(ctx context.Context, rawID string) (*apiclient.LavaFlowRun, error) {
	runID, err := uuid.Parse(rawID)
	if err != nil {
		return nil, fmt.Errorf("invalid run ID %q: expected a UUID", rawID)
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	run, err := authenticated.API.GetLavaFlowRun(ctx, authenticated.ProjectID, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to get LavaFlow run %s: %w", runID, err)
	}
	return run, nil
}

// StartRun queues a run that diagnoses an issue of the current project.
func (s Service) StartRun(ctx context.Context, rawIssueID string) (*apiclient.LavaFlowRun, error) {
	issueID, err := uuid.Parse(rawIssueID)
	if err != nil {
		return nil, fmt.Errorf("invalid issue ID %q: expected a UUID", rawIssueID)
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	run, err := authenticated.API.StartLavaFlowRun(ctx, authenticated.ProjectID, issueID)
	if err != nil {
		return nil, refusal(fmt.Errorf("failed to start a LavaFlow run for issue %s: %w", issueID, err))
	}
	return run, nil
}

// refusal adds what to do about a plan refusal, keeping the refusal so the
// exit code still reports it.
func refusal(err error) error {
	if api.Status(err) == http.StatusForbidden && api.Code(err) == codePlanRequired {
		return api.WithCause(errors.New(err.Error()+"\nLavaFlow needs a paid plan: upgrade the project's plan in the dashboard. "+CostNote), err)
	}
	return err
}
