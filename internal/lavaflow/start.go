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
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// ErrPersonStarts refuses starting a run with a project access token or a
// delegated grant. A run
// sends the project's logs and source code to an AI service and can open a
// pull request, so the API takes it from the owner's own session only.
var ErrPersonStarts = errors.New("starting a LavaFlow run needs the project owner's session: a project access token or a delegated grant can't")

// ErrStartNeedsLiveSignIn reports the start's person_required for a token
// the CLI sent as a person's: a platform token that never expires, or one
// whose sign-in ended. The CLI refuses a project access token or a delegated
// grant before it asks.
var ErrStartNeedsLiveSignIn = errors.New("starting a LavaFlow run needs the project owner's live sign-in")

// ErrStartUnavailable reports an API that serves LavaFlow but can't start a
// run: a platform release older than starting one.
var ErrStartUnavailable = errors.New("this Volcano API can't start LavaFlow runs yet")

// ErrStartRefused reports the start's 403 without a code. It stands for
// refusals the API doesn't tell apart: an owner whose account is read-only or
// inactive, and an owner's plan that couldn't be read.
var ErrStartRefused = errors.New("the LavaFlow run didn't start: a read-only or inactive account can't start one, " +
	"and otherwise the project owner's plan couldn't be read; try again later")

// Codes of the start's refusals.
const (
	codeIssueNotFound          = "issue_not_found"
	codeLavaflowDisabled       = "lavaflow_disabled"
	codeRepositoryRequired     = "repository_required"
	codeRepositoryChanged      = "repository_changed"
	codeRunInProgress          = "lavaflow_run_in_progress"
	codeIssueRunLimitReached   = "issue_run_limit_reached"
	codeProjectRunLimitReached = "project_run_limit_reached"
)

const gitConnectCommand = "volcano git connect"

// StartRun starts a LavaFlow run on one of the current project's issues and
// returns it queued, with the body the API sent it in. It refuses a project
// access token before sending anything.
func (s Service) StartRun(ctx context.Context, issueID string) (*apiclient.LavaflowRun, []byte, error) {
	id, err := uuid.Parse(issueID)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid issue ID %q: use the ID of one of the project's issues", issueID)
	}
	authenticated, err := s.project()
	if err != nil {
		return nil, nil, err
	}
	if config.IsProjectBound(authenticated.Config.Token()) {
		return nil, nil, personStarts(authenticated.Config, config.ErrAccountTokenRequired)
	}
	run, body, err := authenticated.API.CreateLavaflowRun(ctx, authenticated.ProjectID, id)
	if err != nil {
		return nil, nil, s.startRefused(ctx, authenticated, id, err)
	}
	return run, body, nil
}

// startRefused says why the API didn't start the run and what to do about
// it, by the refusal's code, keeping the refusal as the cause for the exit
// code.
func (s Service) startRefused(ctx context.Context, authenticated *clisession.ProjectSession, issueID uuid.UUID, err error) error {
	switch api.Code(err) {
	case codePersonRequired:
		return liveSignIn(authenticated.Config, err)
	case codeIssueNotFound:
		return api.WithCause(fmt.Errorf("the project has no issue %s", issueID), err)
	case codeLavaflowDisabled:
		return api.WithCause(fmt.Errorf("LavaFlow is off for this project. Turn it on with '%s', then run this again",
			cliruntime.CommandPath(s.deps, "lavaflow enable")), err)
	case codeRepositoryRequired:
		return api.WithCause(fmt.Errorf("the project no longer links the repository LavaFlow was turned on for. "+
			"Connect it again with '%s', or turn LavaFlow on again with '%s'",
			gitConnectCommand, cliruntime.CommandPath(s.deps, "lavaflow enable")), err)
	case codeRepositoryChanged:
		return api.WithCause(fmt.Errorf("the project links another repository than LavaFlow was turned on for. "+
			"Turn LavaFlow on again with '%s' to fix issues in it", cliruntime.CommandPath(s.deps, "lavaflow enable")), err)
	case codeRunInProgress:
		return s.runInProgress(ctx, authenticated, issueID, err)
	case codeIssueRunLimitReached, codeProjectRunLimitReached:
		return runLimitReached(time.Now(), err)
	}
	switch {
	case api.Status(err) == http.StatusMethodNotAllowed:
		return api.WithCause(ErrStartUnavailable, err)
	case api.Status(err) == http.StatusForbidden && api.Code(err) == "":
		if message := api.Message(err); message != "" {
			return api.WithCause(fmt.Errorf("%w. Volcano said: %q", ErrStartRefused, message), err)
		}
		return api.WithCause(ErrStartRefused, err)
	}
	return unavailable(fmt.Errorf("failed to start a LavaFlow run: %w", err))
}

// runInProgress names the issue's run in progress when the newest page of
// runs has it, so the person can follow it. A list that fails or doesn't
// have it points to the list instead.
func (s Service) runInProgress(ctx context.Context, authenticated *clisession.ProjectSession, issueID uuid.UUID, cause error) error {
	limit := MaxRunsLimit
	page, _, err := authenticated.API.ListLavaflowRuns(ctx, authenticated.ProjectID, apiclient.ListLavaflowRunsParams{Limit: &limit})
	if err == nil {
		for _, run := range page.Data {
			if run.IssueId == issueID && inProgress(run.Status) {
				return api.WithCause(fmt.Errorf("the issue already has LavaFlow run %s in progress. Follow it with '%s'",
					run.Id, cliruntime.CommandPath(s.deps, "lavaflow show "+run.Id.String())), cause)
			}
		}
	}
	return api.WithCause(fmt.Errorf("the issue already has a LavaFlow run in progress. Find it with '%s'",
		cliruntime.CommandPath(s.deps, "lavaflow list")), cause)
}

func inProgress(status apiclient.LavaflowRunStatus) bool {
	switch status {
	case apiclient.LavaflowRunStatusQueued, apiclient.LavaflowRunStatusDiagnosing,
		apiclient.LavaflowRunStatusFixing, apiclient.LavaflowRunStatusReviewing:
		return true
	}
	return false
}

// runLimitReached says a run limit refused the start, in Volcano's words,
// which count the runs, and when it has room again from the refusal's
// Retry-After.
func runLimitReached(now time.Time, err error) error {
	refusal := "a LavaFlow run limit is reached"
	if message := api.Message(err); message != "" {
		refusal = message
	}
	refusal = "the LavaFlow run didn't start: " + refusal
	// A Retry-After that has already passed on this clock says nothing.
	if at, ok := api.RetryAt(err, now); ok {
		if wait := at.Sub(now).Round(time.Second); wait > 0 {
			return api.WithCause(fmt.Errorf("%s. Try again in %s", refusal, wait), err)
		}
	}
	return api.WithCause(fmt.Errorf("%s. Try again later", refusal), err)
}

// liveSignIn says how the owner signs in so a start counts as theirs. A token
// in VOLCANO_TOKEN wins over a 'volcano login' sign-in, so that case says to
// unset it or replace it.
func liveSignIn(cfg *config.Config, cause error) error {
	how := "Run 'volcano login', or use a platform token that expires"
	if cfg.TokenFromEnv() {
		how = "Unset " + config.TokenEnv + " and run 'volcano login', or set it to a platform token that expires"
	}
	return api.WithCause(fmt.Errorf("%w. %s", ErrStartNeedsLiveSignIn, how), cause)
}

// personStarts says how the owner starts a run instead. cause is the
// refusal, the API's or the CLI's own, which keeps the exit code of a
// permission refused.
func personStarts(cfg *config.Config, cause error) error {
	return api.WithCause(fmt.Errorf("%w. %s", ErrPersonStarts, signInAgain(cfg)), cause)
}
