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
