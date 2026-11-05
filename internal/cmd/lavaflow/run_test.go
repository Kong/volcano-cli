package lavaflow

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

// run start names the issue and the project in the body, and says which run
// started and how to follow it.
func TestRunStartSaysWhichRunStarted(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "run", "start", lavaflowIssueID)
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow run started")
	assert.Contains(t, out, "Run: "+startedRunID+"\n")
	assert.Contains(t, out, "Status: queued\n")
	assert.Contains(t, out, "Follow it with volcano cloud lavaflow show "+startedRunID+"\n")
	assertLavaFlowWording(t, out)
	fake.set(func(s *lavaflowServer) {
		assert.Equal(t, []string{lavaflowIssueID}, s.started)
		assert.Equal(t, []string{lavaflowProjectID}, s.projects)
	})
}

// --json prints the run as the API returned it, fields newer than the CLI's
// contract included.
func TestRunStartJSONIsTheAPIsBody(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "run", "start", lavaflowIssueID, "--json")
	require.NoError(t, err)
	assert.Equal(t, asJSON(t, startedRun(lavaflowIssueID)), decodeJSON(t, out))
}

// An issue ID that isn't a UUID is refused before anything is sent.
func TestRunStartRefusesAnInvalidIssueID(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	_, err := executeLavaflowCommand(t, New(deps), "run", "start", "not-an-id")
	require.Error(t, err)
	assert.Equal(t, `invalid issue ID "not-an-id": use the ID of one of the project's issues`, err.Error())
	fake.set(func(s *lavaflowServer) { assert.Empty(t, s.started) })
}

// A project access token can't start a run, so the CLI says how the owner
// does before it sends anything, with the exit code of a permission refused.
func TestRunStartWithAProjectTokenNeedsTheOwnersSession(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")

	_, err := executeLavaflowCommand(t, New(deps), "run", "start", lavaflowIssueID)
	require.ErrorIs(t, err, clilavaflow.ErrPersonStarts)
	require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired, "the exit code reports a permission refused")
	assert.Equal(t, "starting a LavaFlow run needs the project owner's session: a project access token can't. "+
		"Unset VOLCANO_TOKEN and sign in with 'volcano login', then run this again", err.Error())
	fake.set(func(s *lavaflowServer) { assert.Empty(t, s.started) })
}

// Each refusal says what to do about it, keeping the API's status for the
// exit code. None sends the person to the website.
func TestRunStartSaysWhatToDoAboutARefusal(t *testing.T) {
	for name, tc := range map[string]struct {
		status        int
		message, code string
		want          string
	}{
		"token that never expires": {
			http.StatusForbidden, "this token never expires; sign in with `volcano login` to start a run", "person_required",
			"starting a LavaFlow run needs the project owner's live sign-in. " +
				"Run 'volcano login', or use a platform token that expires",
		},
		"read-only account": {
			http.StatusForbidden, "the account is read-only", "",
			"the LavaFlow run didn't start: a read-only or inactive account can't start one, and otherwise the project " +
				"owner's plan couldn't be read; try again later. Volcano said: \"the account is read-only\"",
		},
		"plan required": {
			http.StatusForbidden, "LavaFlow needs a plan that includes it", "plan_required", lavaflowPlanRequired,
		},
		"not available": {
			http.StatusNotFound, "LavaFlow is not available", "feature_unavailable", lavaflowNotAvailable,
		},
		"unknown issue": {
			http.StatusNotFound, "issue not found", "issue_not_found", "the project has no issue " + lavaflowIssueID,
		},
		"off": {
			http.StatusConflict, "LavaFlow is off for this project; turn it on to start runs", "lavaflow_disabled",
			"LavaFlow is off for this project. Turn it on with 'volcano cloud lavaflow enable', then run this again",
		},
		"repository gone": {
			http.StatusConflict, "the project no longer links the repository LavaFlow was turned on for", "repository_required",
			"the project no longer links the repository LavaFlow was turned on for. Connect it again with " +
				"'volcano git connect', or turn LavaFlow on again with 'volcano cloud lavaflow enable'",
		},
		"repository changed": {
			http.StatusConflict, "the project links another repository than LavaFlow was turned on for", "repository_changed",
			"the project links another repository than LavaFlow was turned on for. Turn LavaFlow on again with " +
				"'volcano cloud lavaflow enable' to fix issues in it",
		},
		"another refusal": {
			http.StatusConflict, "the project is being deleted", "",
			"failed to start a LavaFlow run: HTTP 409: the project is being deleted",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.refuse, s.refuseMethod = tc.status, http.MethodPost
				s.refusal = map[string]any{"error": tc.message}
				if tc.code != "" {
					s.refusal["code"] = tc.code
				}
			})

			_, err := executeLavaflowCommand(t, New(deps), "run", "start", lavaflowIssueID)
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
			assert.Equal(t, tc.status, api.Status(err))
			assertLavaFlowWording(t, err.Error())
		})
	}
}

// A token in VOLCANO_TOKEN is used instead of a 'volcano login' sign-in, so
// a start refused for a token that never expires says to unset or replace it.
func TestRunStartWithATokenThatNeverExpiresNeedsALiveSignIn(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	t.Setenv("VOLCANO_TOKEN", "vol_never_expires")
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusForbidden, http.MethodPost
		s.refusal = map[string]any{"error": "this token never expires", "code": "person_required"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "run", "start", lavaflowIssueID)
	require.ErrorIs(t, err, clilavaflow.ErrStartNeedsLiveSignIn)
	assert.Equal(t, "starting a LavaFlow run needs the project owner's live sign-in. "+
		"Unset VOLCANO_TOKEN and run 'volcano login', or set it to a platform token that expires", err.Error())
	assert.Equal(t, http.StatusForbidden, api.Status(err), "the exit code reports a permission refused")
}

// An issue with a run in progress names that run when the newest runs have
// it, and points to the list when they don't.
func TestRunStartNamesTheRunInProgress(t *testing.T) {
	for name, tc := range map[string]struct {
		status, issue string
		want          string
	}{
		"listed": {
			"fixing", lavaflowIssueID,
			"the issue already has LavaFlow run " + lavaflowRunID + " in progress. " +
				"Follow it with 'volcano cloud lavaflow show " + lavaflowRunID + "'",
		},
		"another issue's": {
			"fixing", "12121212-1212-4212-8212-121212121212",
			"the issue already has a LavaFlow run in progress. Find it with 'volcano cloud lavaflow list'",
		},
		"finished": {
			"pr_open", lavaflowIssueID,
			"the issue already has a LavaFlow run in progress. Find it with 'volcano cloud lavaflow list'",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.refuse, s.refuseMethod = http.StatusConflict, http.MethodPost
				s.refusal = map[string]any{"error": "the issue already has a LavaFlow run in progress", "code": "lavaflow_run_in_progress"}
				s.runs[0]["status"], s.runs[0]["issue_id"] = tc.status, tc.issue
			})

			_, err := executeLavaflowCommand(t, New(deps), "run", "start", lavaflowIssueID)
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
			assert.Equal(t, http.StatusConflict, api.Status(err))
			assert.Equal(t, "100", fake.lastQuery(t, "/runs").Get("limit"))
		})
	}
}

// A run limit says when it has room again, from Retry-After, in Volcano's
// words, which count the runs.
func TestRunStartSaysWhenARunLimitHasRoom(t *testing.T) {
	for _, tc := range []struct {
		code, message, retryAfter string
		want                      string
	}{
		{
			"issue_run_limit_reached", "the issue started 5 LavaFlow runs in the last 24 hours", "5400",
			"the LavaFlow run didn't start: the issue started 5 LavaFlow runs in the last 24 hours. Try again in 1h30m0s",
		},
		{
			"project_run_limit_reached", "the project started 200 LavaFlow runs in the last 24 hours", "",
			"the LavaFlow run didn't start: the project started 200 LavaFlow runs in the last 24 hours. Try again later",
		},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = http.StatusTooManyRequests, http.MethodPost
			s.refusal = map[string]any{"error": tc.message, "code": tc.code}
			if tc.retryAfter != "" {
				s.refusalHeaders = map[string]string{"Retry-After": tc.retryAfter}
			}
		})

		_, err := executeLavaflowCommand(t, New(deps), "run", "start", lavaflowIssueID)
		require.Error(t, err, tc.code)
		assert.Equal(t, tc.want, err.Error(), tc.code)
		assert.Equal(t, http.StatusTooManyRequests, api.Status(err), tc.code)
	}
}

// A platform release that lists runs but can't start one answers the start
// with the router's bare 405. The CLI says so.
func TestRunStartSaysWhenTheAPICantStartRuns(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.startMissing = true })

	_, err := executeLavaflowCommand(t, New(deps), "run", "start", lavaflowIssueID)
	require.ErrorIs(t, err, clilavaflow.ErrStartUnavailable)
	assert.Equal(t, "this Volcano API can't start LavaFlow runs yet", err.Error())
	assert.Equal(t, http.StatusMethodNotAllowed, api.Status(err))
}
