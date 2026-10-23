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
		"Issue: TypeError: cart is undefined (" + lavaflowIssueID + ")",
		"Status: succeeded",
		"Pull request: https://github.com/acme/shop/pull/42 (open)",
		"Summary: Checkout reads the cart before it loads.",
		"Root cause: handleCheckout dereferences cart without a guard.",
		"Suggested fix: Return 409 until the cart has loaded.",
		"Confidence: high",
		"Files: functions/checkout.ts",
		"Evidence: log (2026-10-23T08:59:00Z): TypeError: cart is undefined",
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
	run["issue_title"] = "boom\x1b[2J"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.NotContains(t, out, "\x1b")
}

func TestRunsGetSaysWhyNoPullRequestOpened(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	run := fixedRun()
	delete(run, "pull_request")
	run["pull_request_skipped"] = "not_fixable"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Pull request: none: a code change would not fix it")
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
