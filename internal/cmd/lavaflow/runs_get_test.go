package lavaflow

import (
	"encoding/json"
	"net/http"
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

func TestRunsGetSaysWhyNoPullRequestOpened(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	for reason, want := range map[string]string{
		"not_fixable":        "Pull request: none: a code change would not fix it",
		"source_unavailable": "Pull request: none: LavaFlow couldn't read the deployed source",
	} {
		run := fixedRun()
		delete(run, "pull_request")
		run["pull_request_skipped"] = reason
		fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

		out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
		require.NoError(t, err)
		assert.Contains(t, out, want)
	}
}

// A canceled run may have started before LavaFlow was turned off, and a
// paused run waits for LavaFlow to resume.
func TestRunsGetSaysWhyARunStoppedOrWaits(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	canceled := queuedRun()
	canceled["status"] = "canceled"
	canceled["started_at"] = "2026-10-23T09:00:05Z"
	canceled["failure_code"] = "lavaflow_disabled"
	paused := queuedRun()
	paused["failure_code"] = "feature_paused"

	for _, tc := range []struct {
		run  map[string]any
		want string
	}{
		{canceled, "Reason: LavaFlow was turned off before the run finished"},
		{paused, "Reason: waiting: LavaFlow is paused. This run will start when LavaFlow resumes"},
	} {
		fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{tc.run} })
		out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
		require.NoError(t, err)
		assert.Contains(t, out, tc.want)
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
// not know is still explained, never left blank.
func TestRunsGetExplainsValuesItDoesNotKnow(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	run := fixedRun()
	delete(run, "pull_request")
	run["pull_request_skipped"] = "source_not_committed"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Pull request: none: source not committed")

	failed := queuedRun()
	failed["status"] = "failed"
	failed["failure_code"] = "brand_new_code"
	failed["failure_message"] = "Something new\x1b[2J went wrong"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{failed} })
	out, err = executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Reason: Something new")
	assert.NotContains(t, out, "\x1b")

	delete(failed, "failure_message")
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{failed} })
	out, err = executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Reason: brand new code")
}
