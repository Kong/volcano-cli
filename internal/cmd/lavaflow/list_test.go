package lavaflow

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListShowsEachRunNewestFirst(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Equal(t, url.Values{"limit": {"25"}}, fake.lastQuery(t, "/runs"))
	assert.Regexp(t, lavaflowRunID+`\s+PR open\s+checkout\s+TypeError: Cannot read prop\.\.\.\s+\S+ \S+\s+#42 open\n`, out)
	assert.Regexp(t, lavaflowFailedID+`\s+failed\s+checkout\s+-\s+\S+ \S+\s+-\n`, out)
	assert.Contains(t, out, "Showing 2 run(s)")
	assert.NotContains(t, out, "--cursor", "the last page names no next one")
	assertLavaFlowWording(t, out)
}

// The filters reach the API, and the next page keeps them.
func TestListFiltersAndPages(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.nextCursor = "next-page" })

	out, err := executeLavaflowCommand(t, New(deps), "list", "--status", "failed", "--resource", lavaflowResourceID,
		"--limit", "2", "--cursor", "this-page")
	require.NoError(t, err)
	assert.Equal(t, url.Values{
		"status": {"failed"}, "resource_id": {lavaflowResourceID}, "limit": {"2"}, "cursor": {"this-page"},
	}, fake.lastQuery(t, "/runs"))
	assert.Contains(t, out, "volcano cloud lavaflow list --limit 2 --cursor next-page --status failed --resource "+lavaflowResourceID)
}

func TestListRefusesABadFilterBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--resource", "checkout"}, `invalid --resource "checkout": use a resource ID from 'volcano cloud lavaflow resources --json'`},
		{[]string{"--limit", "0"}, "--limit must be between 1 and 100"},
		{[]string{"--limit", "101"}, "--limit must be between 1 and 100"},
		{[]string{"--status", "succeeded"}, `invalid --status "succeeded": use one of queued, diagnosing, fixing, ` +
			"reviewing, pr_open, completed, failed, cancelled"},
	} {
		fake, deps := newLavaflowServer(t)

		_, err := executeLavaflowCommand(t, New(deps), append([]string{"list"}, tc.args...)...)
		require.EqualError(t, err, tc.want, "%q", tc.args)
		assert.Empty(t, fake.queries["/runs"])
	}
}

func TestListSaysWhenThereAreNoRuns(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs = nil })

	out, err := executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Equal(t, "No runs\n", out)
}

// A running run says which stage it is in, in list and show alike, and a
// status the CLI doesn't know prints as the API gives it.
func TestARunsStatusSaysWhatItIsDoing(t *testing.T) {
	for status, want := range map[string]string{
		"queued":       "queued",
		"diagnosing":   "Running: finding the root cause",
		"fixing":       "Running: writing the fix",
		"reviewing":    "Running: reviewing the fix",
		"pr_open":      "PR open",
		"completed":    "completed",
		"waiting_room": "waiting_room",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.runs[0]["status"] = status })

		out, err := executeLavaflowCommand(t, New(deps), "list")
		require.NoError(t, err, status)
		assert.Contains(t, out, lavaflowRunID+"  "+want+" ", status)

		out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, status)
		assert.Contains(t, out, "Status: "+want+"\n", status)
	}
}

// A run whose merge waits for a person says so in the list; one past its
// approval reads as its run status.
func TestListMarksARunWaitingForApproval(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.waitForApproval(approvalReason("automatic_run", nil)) })

	out, err := executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Contains(t, out, lavaflowRunID+"  Waiting for your approval ")

	fake.set(func(s *lavaflowServer) { s.runs[0]["merge"].(map[string]any)["status"] = "waiting_for_checks" })
	out, err = executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Contains(t, out, lavaflowRunID+"  PR open ")
}
