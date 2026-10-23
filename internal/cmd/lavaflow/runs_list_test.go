package lavaflow

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunsListShowsEachRunsOutcome(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	paused := queuedRun()
	paused["id"] = "66666666-6666-4666-8666-666666666666"
	paused["trigger"] = issueTrigger("manual", lavaflowIssueID, "Timeout calling payments")
	paused["failure_code"] = "feature_paused"
	failed := queuedRun()
	failed["id"] = "77777777-7777-4777-8777-777777777777"
	failed["trigger"] = issueTrigger("regression", lavaflowIssueID, "")
	failed["status"] = "failed"
	failed["failure_code"] = "model_unavailable"
	failed["failure_message"] = "The model provider was unavailable."
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{fixedRun(), paused, failed} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "list")
	require.NoError(t, err)
	assert.Contains(t, out, lavaflowRunID)
	assert.Contains(t, out, "TypeError: cart is undefined")
	assert.Contains(t, out, "new issue")
	assert.Contains(t, out, "PR #42 open")
	assert.Contains(t, out, "waiting: Volcano paused")
	assert.Contains(t, out, lavaflowIssueID, "a run without a title shows its issue ID")
	assert.Contains(t, out, "regression")
	assertLavaFlowWording(t, out)
}

func TestRunsListPagesWithTheCursor(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs = []map[string]any{fixedRun()}
		s.nextCursor = "next-page"
	})

	out, err := executeLavaflowCommand(t, New(deps), "runs", "list", "--limit", "5")
	require.NoError(t, err)
	assert.Contains(t, out, "Next page: volcano cloud lavaflow runs list --limit 5 --cursor next-page")

	_, err = executeLavaflowCommand(t, New(deps), "runs", "list", "--limit", "5", "--cursor", "next-page")
	require.NoError(t, err)
	fake.set(func(s *lavaflowServer) {
		require.Len(t, s.listQueries, 2)
		assert.Equal(t, url.Values{"limit": {"5"}}, s.listQueries[0])
		assert.Equal(t, url.Values{"limit": {"5"}, "cursor": {"next-page"}}, s.listQueries[1])
	})
}

func TestRunsListRefusesALimitOutOfRange(t *testing.T) {
	_, deps := newLavaflowServer(t)

	for _, limit := range []string{"0", "101"} {
		_, err := executeLavaflowCommand(t, New(deps), "runs", "list", "--limit", limit)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--limit must be between 1 and 100")
	}
}

func TestRunsListEmptyAndJSON(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "runs", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No LavaFlow runs")

	out, err = executeLavaflowCommand(t, New(deps), "runs", "list", "--json")
	require.NoError(t, err)
	var page map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &page))
	assert.Equal(t, false, page["has_more"])
}

// A trigger type this CLI does not know renders as "other", without an issue.
func TestRunsListRendersAnUnknownTriggerAsOther(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	run := queuedRun()
	run["trigger"] = map[string]any{"type": "pull_request", "pull_request": map[string]any{"number": 7}}
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{run} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "other")
	assert.NotContains(t, out, lavaflowIssueID)

	out, err = executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Trigger: other")
	assert.NotContains(t, out, "Issue:")
}

// Only a queued run retries; a failed run with the same code is done.
func TestRunsListSaysRetryingOnlyForQueuedRuns(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	retrying := queuedRun()
	retrying["failure_code"] = "model_unavailable"
	failed := queuedRun()
	failed["id"] = "77777777-7777-4777-8777-777777777777"
	failed["status"] = "failed"
	failed["failure_code"] = "model_unavailable"
	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{retrying} })

	out, err := executeLavaflowCommand(t, New(deps), "runs", "get", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Reason: LavaFlow was busy; retrying")

	fake.set(func(s *lavaflowServer) { s.runs = []map[string]any{failed} })
	out, err = executeLavaflowCommand(t, New(deps), "runs", "get", "77777777-7777-4777-8777-777777777777")
	require.NoError(t, err)
	assert.Contains(t, out, "Reason: LavaFlow was busy\n")
}
