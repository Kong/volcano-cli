// Package lavaflow performs the LavaFlow workflows behind the volcano cloud
// lavaflow subcommands. LavaFlow reads a project's runtime errors, diagnoses
// the issues they group into, and opens pull requests that fix them.
package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// CostNote is what LavaFlow needs during the preview, worded as the dashboard,
// the pricing page and the public docs word it. It names no billing model.
const CostNote = "LavaFlow is in preview. During the preview, it needs the SUPERAGENT plan."

// OnDescription says what LavaFlow does, and that it uses AI to do it, in
// the wording approved for that disclosure. Keep it verbatim: it opens the
// help, enable's output and the docs.
const OnDescription = "LavaFlow uses AI to diagnose this project's errors from their logs and deployed source, " +
	"and opens fix pull requests in your connected repository."

// ErrUnavailable reports an API where LavaFlow is not on: a platform release
// older than LavaFlow, which has none of its routes, or one where Volcano has
// not turned it on or has switched it off. Said in place of the 404 either
// answers, which otherwise reads as a missing project or run.
var ErrUnavailable = errors.New("LavaFlow isn't available on this Volcano API")

// codePlanRequired is the code of a refusal the owner's plan causes.
const codePlanRequired = "plan_required"

// codeFeatureUnavailable is the code of a refusal for LavaFlow being off on
// this API.
const codeFeatureUnavailable = "feature_unavailable"

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
	now      func() time.Time
}

// NewService returns a LavaFlow service.
func NewService(deps cliruntime.Deps) Service {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return Service{sessions: clisession.NewFactory(deps), now: now}
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
		return nil, unavailable(fmt.Errorf("failed to get LavaFlow settings: %w", err))
	}
	status := &Status{Settings: settings}
	status.RepositoryState, status.Repository, err = repository(ctx, authenticated)
	if err != nil {
		return nil, err
	}
	return status, nil
}

// Repository says which repository LavaFlow's fix pull requests go to, for a
// project whose LavaFlow settings were just read or saved: its state, and its
// full name when it is connected. Only a cancel or deadline is an error.
func (s Service) Repository(ctx context.Context) (state, name string, err error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return "", "", err
	}
	return repository(ctx, authenticated)
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
		// The LavaFlow read or write before it proved the project exists, so a
		// 404 means no repository; anything else leaves the connection unknown.
		return RepositoryNotConnected, "", nil
	default:
		return RepositoryUnknown, "", nil
	}
}

// SetEnabled turns LavaFlow on or off for the current project. The update
// changes only the fields it sends, so it sends enabled alone: a manifest's
// max_stage and auto_trigger stay as they are.
func (s Service) SetEnabled(ctx context.Context, enabled bool) (*apiclient.LavaFlowSettings, error) {
	verb := "turn LavaFlow off"
	if enabled {
		verb = "turn LavaFlow on"
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	settings, err := authenticated.API.UpdateLavaFlowSettings(ctx, authenticated.ProjectID,
		apiclient.UpdateLavaFlowSettingsRequest{Enabled: &enabled})
	if err != nil {
		return nil, unavailable(refusal(fmt.Errorf("failed to %s: %w", verb, err)))
	}
	return settings, nil
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
		return nil, unavailable(fmt.Errorf("failed to list LavaFlow runs: %w", err))
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
		return nil, unavailable(fmt.Errorf("failed to get LavaFlow run %s: %w", runID, err))
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
		return nil, unavailable(retryHint(refusal(fmt.Errorf("failed to start a LavaFlow run for issue %s: %w", issueID, err)), s.now()))
	}
	return run, nil
}

// unavailable replaces a refusal that says LavaFlow is not on with
// ErrUnavailable, keeping the refusal so the exit code still reports it.
func unavailable(err error) error {
	if api.RouteMissing(err) || api.Code(err) == codeFeatureUnavailable {
		return api.WithCause(ErrUnavailable, err)
	}
	return err
}

// refusal adds what to do about a plan refusal, keeping the refusal so the
// exit code still reports it.
func refusal(err error) error {
	if api.Status(err) == http.StatusForbidden && api.Code(err) == codePlanRequired {
		return api.WithCause(errors.New(err.Error()+"\nLavaFlow needs a paid plan: upgrade the project's plan in the dashboard. "+CostNote), err)
	}
	return err
}

// retryHint adds when to try again to a refusal whose response said, such as
// a run limit, keeping the refusal so the exit code still reports it. The CLI
// never retries on its own: every run counts toward the project's usage.
func retryHint(err error, now time.Time) error {
	at, ok := api.RetryAt(err, now)
	if !ok {
		return err
	}
	return api.WithCause(errors.New(err.Error()+"\n"+tryAgain(at, now)), err)
}

func tryAgain(at, now time.Time) string {
	if at.Sub(now) < time.Minute {
		return "Try again in a minute."
	}
	// Round up to the minute, so neither the time nor the wait named is early.
	at = at.Add(time.Minute - time.Nanosecond).Truncate(time.Minute)
	minutes := int((at.Sub(now) + time.Minute - time.Nanosecond) / time.Minute)
	in := fmt.Sprintf("%dm", minutes)
	if minutes >= 60 {
		in = fmt.Sprintf("%dh%02dm", minutes/60, minutes%60)
	}
	return fmt.Sprintf("Try again after %s (in %s).", at.In(now.Location()).Format("2006-01-02 15:04 MST"), in)
}
