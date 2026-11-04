package issues

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliissues "github.com/Kong/volcano-cli/internal/issues"
)

func TestListShowsEachIssueMostRecentlySeenFirst(t *testing.T) {
	fake, deps := newIssuesServer(t)

	out, _, err := execute(t, New(deps), nil, "list")
	require.NoError(t, err)
	assert.Equal(t, url.Values{"limit": {"25"}}, fake.last(t).query)
	assert.Regexp(t, testIssueID+`\s+open\s+checkout\s+TypeError: Cannot read properties of \.\.\.\s+42\s+2026-10-13 11:47 UTC\n`, out)
	assert.Regexp(t, testOtherIssue+`\s+resolved\s+storefront\s+connection refused\s+3\s+2026-10-11 08:00 UTC\n`, out)
	assert.Contains(t, out, "Showing 2 issues")
	assert.NotContains(t, out, "--cursor", "the last page names no next one")
}

// The status filter reaches the API, and the next page keeps it.
func TestListFiltersAndPages(t *testing.T) {
	fake, deps := newIssuesServer(t)
	fake.set(func(s *issuesServer) { s.next = "next-page"; s.issues = s.issues[:1] })

	out, _, err := execute(t, New(deps), nil, "list", "--status", "open", "--limit", "1", "--cursor", "this-page")
	require.NoError(t, err)
	assert.Equal(t, url.Values{"status": {"open"}, "limit": {"1"}, "cursor": {"this-page"}}, fake.last(t).query)
	assert.Contains(t, out, "Showing 1 issue\n")
	assert.Contains(t, out, "volcano cloud issues list --limit 1 --cursor next-page --status open")
}

func TestListRefusesABadFilterBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--status", "closed"}, `invalid --status "closed": use open, resolved or ignored`},
		{[]string{"--limit", "0"}, "--limit must be between 1 and 100"},
		{[]string{"--limit", "101"}, "--limit must be between 1 and 100"},
	} {
		fake, deps := newIssuesServer(t)

		_, _, err := execute(t, New(deps), nil, append([]string{"list"}, tc.args...)...)
		require.EqualError(t, err, tc.want, "%q", tc.args)
		assert.Empty(t, fake.sent())
	}
}

func TestListSaysWhenThereAreNoIssues(t *testing.T) {
	fake, deps := newIssuesServer(t)
	fake.set(func(s *issuesServer) { s.issues = nil })

	out, _, err := execute(t, New(deps), nil, "list")
	require.NoError(t, err)
	assert.Equal(t, "No issues\n", out)
}

func TestListJSONIsTheAPIsPage(t *testing.T) {
	_, deps := newIssuesServer(t)

	out, _, err := execute(t, New(deps), nil, "list", "--json")
	require.NoError(t, err)
	page := decodeJSON(t, out)
	assert.Equal(t, false, page["has_more"])
	require.Len(t, page["data"], 2)
	assert.Equal(t, testIssueTitle, page["data"].([]any)[0].(map[string]any)["title"])
}

func TestGetShowsTheIssue(t *testing.T) {
	fake, deps := newIssuesServer(t)

	out, _, err := execute(t, New(deps), nil, "get", testIssueID)
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+testProjectID+"/issues/"+testIssueID, fake.last(t).path)
	for _, line := range []string{
		"Issue: " + testIssueID, "Title: " + testIssueTitle, "Status: open", "Resource: function checkout",
		"Error type: TypeError", "Culprit: chargeCustomer (src/payments.js)", "Exception: yes", "Events: 42",
		"First seen: 2026-10-13 09:12 UTC", "Last seen: 2026-10-13 11:47 UTC", "Last region: us-east-1",
		"Read its notes with volcano cloud issues notes " + testIssueID + ".",
	} {
		assert.Contains(t, out, line+"\n")
	}
	assert.NotContains(t, out, "Resolved:")
}

func TestGetShowsWhenAnIssueWasResolvedAndReopened(t *testing.T) {
	_, deps := newIssuesServer(t)

	out, _, err := execute(t, New(deps), nil, "get", testOtherIssue)
	require.NoError(t, err)
	assert.Contains(t, out, "Resource: frontend storefront\n")
	assert.Contains(t, out, "Exception: no\n")
	assert.Contains(t, out, "Resolved: 2026-10-12 08:00 UTC\n")
	assert.Contains(t, out, "Reopened: 2026-10-11 07:00 UTC\n")
	assert.NotContains(t, out, "Error type:")
}

// Text from logs prints as plain text: a control character can't move the
// cursor or change colors.
func TestGetPrintsTheTitleAsPlainText(t *testing.T) {
	fake, deps := newIssuesServer(t)
	fake.set(func(s *issuesServer) { s.issues[0]["title"] = "Error: \x1b[31mred\x1b[0m \u009bx" })

	out, _, err := execute(t, New(deps), nil, "get", testIssueID)
	require.NoError(t, err)
	assert.Contains(t, out, `Title: Error: \x1b[31mred\x1b[0m \u009bx`)

	out, _, err = execute(t, New(deps), nil, "get", testIssueID, "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"Error: \u001b[31mred\u001b[0m \u009bx"`)
	assert.Equal(t, "Error: \x1b[31mred\x1b[0m \u009bx", decodeJSON(t, out)["title"])
}

func TestGetRefusesAnIDThatIsNotOne(t *testing.T) {
	fake, deps := newIssuesServer(t)

	_, _, err := execute(t, New(deps), nil, "get", "checkout")
	require.EqualError(t, err, `invalid issue ID "checkout": use an ID from 'volcano cloud issues list'`)
	assert.Empty(t, fake.sent())
}

func TestGetSaysAnUnknownIssueIsNotFound(t *testing.T) {
	_, deps := newIssuesServer(t)

	_, _, err := execute(t, New(deps), nil, "get", "00000000-0000-4000-8000-000000000000")
	require.EqualError(t, err, "failed to get issue 00000000-0000-4000-8000-000000000000: HTTP 404: issue not found")
	assert.Equal(t, http.StatusNotFound, api.Status(err))
}

// Where issues aren't available, every read says so, keeping the 404 for the
// exit code.
func TestReadsSayWhenIssuesAreNotAvailable(t *testing.T) {
	for _, args := range [][]string{{"list"}, {"get", testIssueID}, {"notes", testIssueID}} {
		fake, deps := newIssuesServer(t)
		fake.set(func(s *issuesServer) {
			s.refuse = http.StatusNotFound
			s.refusal = map[string]any{"error": "issues are not available", "code": "feature_unavailable"}
		})

		_, _, err := execute(t, New(deps), nil, args...)
		require.ErrorIs(t, err, cliissues.ErrNotAvailable, "%q", args)
		assert.EqualError(t, err, "issues aren't available in this Volcano environment yet")
		assert.Equal(t, http.StatusNotFound, api.Status(err))
	}
}

// A platform release without the routes answers its router's bare 404.
func TestReadsSayWhenTheAPIHasNoIssues(t *testing.T) {
	for args, want := range map[string]error{"list": cliissues.ErrUnavailable, "notes": cliissues.ErrNotesUnavailable} {
		fake, deps := newIssuesServer(t)
		fake.set(func(s *issuesServer) { s.routesMissing = true })

		command := []string{args}
		if args == "notes" {
			command = append(command, testIssueID)
		}
		_, _, err := execute(t, New(deps), nil, command...)
		require.ErrorIs(t, err, want, args)
	}
}

func TestAnUnknownSubcommandIsAnError(t *testing.T) {
	_, deps := newIssuesServer(t)

	_, _, err := execute(t, New(deps), nil, "show", testIssueID)
	require.EqualError(t, err, `unknown command "show" for "issues"`)
}

// Local development records issues as the cloud does, so its reads work.
// Notes are input to LavaFlow, which it doesn't run, so every notes command
// refuses without asking the local server.
func TestLocalReadsIssuesAndLeavesNotesToTheCloud(t *testing.T) {
	fake, deps := newIssuesServer(t)
	deps.CommandPathPrefix = "volcano"
	deps.LocalMode = true
	fake.set(func(s *issuesServer) { s.local = true })

	out, _, err := execute(t, NewLocal(deps), nil, "get", testIssueID)
	require.NoError(t, err)
	assert.Contains(t, out, "Title: "+testIssueTitle)
	assert.NotContains(t, out, "notes", "local development has no notes to point to")

	help, _, err := execute(t, NewLocal(deps), nil, "--help")
	require.NoError(t, err)
	assert.NotContains(t, help, "  notes ")
	assert.Contains(t, help, "volcano cloud issues notes")

	sent := len(fake.sent())
	for _, args := range [][]string{
		{"notes", testIssueID},
		{"notes", "add", testIssueID, "--text", "a note"},
		{"notes", "edit", testNoteID, "--text", "a note"},
		{"notes", "delete", testNoteID, "--yes"},
	} {
		_, _, err := execute(t, NewLocal(deps), nil, args...)
		require.EqualError(t, err, `"issues notes" is a cloud command: notes are input to LavaFlow, which runs on `+
			`Volcano's cloud service and not in local development, so run 'volcano cloud issues notes ...' instead`, "%q", args)
	}
	assert.Len(t, fake.sent(), sent, "the local server was asked")
}
