package lavaflow

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
)

func TestRunsGetShowsTheDiagnosisAndPullRequest(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{fixedRun()} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	for _, want := range []string{
		"Issue: " + lavaflowIssueTitle + " (" + lavaflowIssueID + ")",
		"Status: succeeded",
		"Pull request: https://github.com/acme/shop/pull/42 (open)",
		"Summary: Checkout reads the cart before it loads.",
		"Root cause: handleCheckout dereferences cart without a guard.",
		"Suggested fix: Return 409 until the cart has loaded.",
		"Confidence: high",
		"Files: functions/checkout.ts",
		"Evidence: log (2026-10-23T08:59:00Z): TypeError: Cannot read properties of undefined (reading 'items')",
	} {
		assert.Contains(t, out, want)
	}
	assertLavaFlowWording(t, out)
}

// The diagnosis is the run's own text, so terminal control sequences in it
// never reach the terminal.
func TestRunsGetStripsControlCharacters(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	run := fixedRun()
	run["trigger"] = issueTrigger("manual", lavaflowIssueID, "boom\x1b[2J")
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.NotContains(t, out, "\x1b")
}

// The diagnosis can span lines, such as a stack trace; it keeps them,
// indented under the label.
func TestRunsGetKeepsTheDiagnosisLineBreaks(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	run := fixedRun()
	diagnosis := run["diagnosis"].(map[string]any)
	diagnosis["suggested_fix"] = "Guard the cart.\r\n\n1. Return 409\x1b[2J until it loads."
	diagnosis["evidence"] = []any{map[string]any{"kind": "log", "detail": "TypeError\n    at checkout.ts:12"}}
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Suggested fix: Guard the cart.\n\n  1. Return 409[2J until it loads.\n")
	assert.Contains(t, out, "Evidence: log: TypeError\n      at checkout.ts:12\n")
	assert.NotContains(t, out, "\x1b")
	assert.NotContains(t, out, "\r")
}

// An issue without a title is shown by its ID alone.
func TestRunsGetShowsAnUntitledIssueByID(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	run := fixedRun()
	run["trigger"] = issueTrigger("manual", lavaflowIssueID, "")
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Issue: "+lavaflowIssueID+"\n")
}

func TestRunsGetSaysWhyNoPullRequestOpenedAndWhatToDo(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	for reason, want := range map[string][]string{
		"not_fixable": {
			"Pull request: none: a code change would not fix it\n",
			"\nNext: Act on the diagnosis above; the fix is outside the code.\n",
		},
		"source_unavailable": {
			"Pull request: none: LavaFlow couldn't read the deployed source\n",
			"\nNext: Connect the repository (volcano git connect) and deploy by pushing to it. " +
				"LavaFlow fixes code that a push to the connected repository deployed.\n",
		},
		"repository_changed": {
			"Pull request: none: the connected repository changed during the run\n",
			"\nNext: Start a new run: volcano cloud lavaflow runs start --issue " + lavaflowIssueID + "\n",
		},
	} {
		run := fixedRun()
		delete(run, "pull_request")
		run["pull_request_skipped"] = reason
		fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

		out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
		require.NoError(t, err)
		for _, w := range want {
			assert.Contains(t, out, w, reason)
		}
		assert.True(t, strings.HasSuffix(out, want[1]), "%s: the next step comes last, after the diagnosis", reason)
	}
}

// Every reason the API documents for a run that stopped or opened no pull
// request is explained in words, with what to do, and never by its code. The
// runs list fits it in its Result column.
func TestEveryStopIsExplainedWithWhatToDo(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	var runs []map[string]any
	for _, code := range []string{
		"feature_unavailable", "feature_paused", "model_request_rejected", "model_unavailable", "model_refused",
		"no_diagnosis", "budget_exhausted", "timed_out", "attempts_exhausted", "lavaflow_disabled", "plan_required",
		"issue_not_found", "internal_error",
		"autofix_disabled", "api_key_rejected", "settings_changed", "managed_models_unavailable",
	} {
		run := queuedRun()
		run["status"] = "failed"
		if strings.HasPrefix(code, "feature_") {
			run["status"] = "queued"
		}
		run["failure_code"] = code
		run["code"] = code
		runs = append(runs, run)
	}
	for _, reason := range []string{
		"source_unavailable", "pull_request_open", "not_fixable", "low_confidence", "no_patch",
		"permission_missing", "repository_changed", "publish_failed",
	} {
		run := fixedRun()
		delete(run, "pull_request")
		run["pull_request_skipped"] = reason
		run["code"] = reason
		runs = append(runs, run)
	}

	for _, run := range runs {
		code := run["code"].(string)
		fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

		out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
		require.NoError(t, err, code)
		assert.Regexp(t, `\n(Reason|Pull request): (none: )?[A-Za-z]`, out, code)
		assert.Regexp(t, `\n\nNext: [A-Z][^\n]+\n$`, out, code)
		assert.NotContains(t, out, code)
		assertLavaFlowWording(t, out)

		out, err = executeLavaflowCommand(t, New(deps), "runs", "list")
		require.NoError(t, err, code)
		assert.NotRegexp(t, `(queued|failed|succeeded) +[^\n]*\.\.\.`, out, "%s: the result fits its column", code)
		assertLavaFlowWording(t, out)
	}
}

// A canceled run may have started before LavaFlow was turned off, and a
// paused run starts when LavaFlow resumes.
func TestRunsGetSaysWhyARunStoppedOrIsPaused(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	canceled := queuedRun()
	canceled["status"] = "canceled"
	canceled["started_at"] = "2026-10-23T09:00:05Z"
	canceled["failure_code"] = "lavaflow_disabled"
	paused := queuedRun()
	paused["failure_code"] = "feature_paused"
	unavailable := queuedRun()
	unavailable["failure_code"] = "feature_unavailable"

	for _, tc := range []struct {
		run          map[string]any
		reason, next string
	}{
		{
			canceled, "LavaFlow was turned off before the run finished",
			"Turn LavaFlow on (volcano cloud lavaflow enable), then start a new run.",
		},
		{paused, "paused: LavaFlow is paused", "Nothing to do: the run carries on when LavaFlow resumes."},
		{unavailable, "paused: LavaFlow is unavailable here", "Nothing to do: the run resumes when LavaFlow is available again."},
	} {
		fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{tc.run} })
		out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
		require.NoError(t, err)
		assert.Contains(t, out, "\nReason: "+tc.reason+"\n")
		assert.True(t, strings.HasSuffix(out, "\n\nNext: "+tc.next+"\n"), "the next step comes last: %q", out)
	}
}

// A diagnose-only run has neither a pull request nor a skipped reason.
func TestRunsGetSaysADiagnoseOnlyRunOpensNoPullRequest(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	run := fixedRun()
	delete(run, "pull_request")
	run["max_stage"] = "diagnose"
	run["stage_reached"] = "diagnose"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Pull request: none: the run only diagnoses (max_stage: diagnose)")
	assert.Contains(t, out, "\nNext: For fix pull requests, set max_stage: pull_request in the project configuration.\n")
}

func TestRunsGetJSONKeepsTheRawRun(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{fixedRun()} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID, "--json")
	require.NoError(t, err)
	var run map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &run))
	assert.Equal(t, "succeeded", run["status"])
}

func TestRunsGetRefusesAnInvalidIDAndReportsAMissingRun(t *testing.T) {
	_, deps := newLavaflowServer(t)

	_, err := executeLavaflowCommand(t, New(deps), "runs", "get", "not-a-uuid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid run ID "not-a-uuid"`)

	_, err = executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "run not found")
	assert.Equal(t, http.StatusNotFound, api.Status(err))
}

// Volcano adds failure codes and skipped reasons over time. One this CLI does
// not know is still explained in words, never by its code alone, and still
// says what to do.
func TestRunsGetExplainsValuesItDoesNotKnow(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	run := fixedRun()
	delete(run, "pull_request")
	run["pull_request_skipped"] = "source_not_committed"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Pull request: none: LavaFlow did not open one (reason: source_not_committed)\n")
	assert.Contains(t, out, "\nNext: Act on the diagnosis above.\n")

	failed := queuedRun()
	failed["status"] = "failed"
	failed["failure_code"] = "brand_new_code"
	failed["failure_message"] = "Something new\x1b[2J went wrong"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{failed} })
	out, err = executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Reason: Something new")
	assert.Contains(t, out, "\nNext: Start a new run: volcano cloud lavaflow runs start --issue "+lavaflowIssueID+"\n")
	assert.NotContains(t, out, "\x1b")

	delete(failed, "failure_message")
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{failed} })
	out, err = executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Reason: the run stopped (code: brand_new_code)\n")
	assert.Contains(t, out, "\nNext: Start a new run: volcano cloud lavaflow runs start --issue "+lavaflowIssueID+"\n")

	failed["status"] = "canceled"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{failed} })
	out, err = executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Reason: the run was canceled (code: brand_new_code)\n")

	failed["status"] = "queued"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{failed} })
	out, err = executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Reason: retrying: the last attempt failed (code: brand_new_code)\n")
	assert.Contains(t, out, "\nNext: Nothing to do: LavaFlow tries again on its own.\n")
}

// A running run's failure code is from an attempt it has moved past, and a
// run with a pull request has nothing left to do.
func TestRunsGetSaysNothingAboutAnAttemptTheRunMovedPast(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	running := queuedRun()
	running["status"] = "running"
	running["failure_code"] = "timed_out"
	for _, run := range []map[string]any{running, fixedRun()} {
		fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

		out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
		require.NoError(t, err)
		assert.NotContains(t, out, "Reason:")
		assert.NotContains(t, out, "Next:")
	}
}
