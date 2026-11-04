package issues

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliissues "github.com/Kong/volcano-cli/internal/issues"
)

const nextRunReads = "LavaFlow reads this note on the issue's next run."

func TestNotesListsAnIssuesThreadNewestFirst(t *testing.T) {
	fake, deps := newIssuesServer(t)

	out, _, err := execute(t, New(deps), nil, "notes", testIssueID)
	require.NoError(t, err)
	sent := fake.last(t)
	assert.Equal(t, "/issue-notes", sent.path)
	assert.Equal(t, url.Values{"project_id": {testProjectID}, "issue_id": {testIssueID}, "limit": {"25"}}, sent.query)
	assert.Equal(t, testNoteID+"  on the issue  by owner@example.com  2026-11-04 19:48 UTC  edited 2026-11-04 20:01 UTC, version 2\n"+
		"  Coupons without an expiry never expire.\n"+
		"  SAVE10 is one.\n"+
		"\n"+
		testRunNoteID+"  on run "+testRunID+"  by a deleted account  2026-11-04 19:40 UTC\n"+
		"  Closed the pull request: the fix belongs in the cart.\n"+
		"\n"+
		"Showing 2 notes\n", out)
}

// A project access token reads authors by user ID alone.
func TestNotesNameAnAuthorWithoutAnEmailByID(t *testing.T) {
	fake, deps := newIssuesServer(t)
	fake.set(func(s *issuesServer) {
		s.notes = s.notes[:1]
		s.notes[0]["author"] = map[string]any{"id": testAuthorID}
	})

	out, _, err := execute(t, New(deps), nil, "notes", testIssueID)
	require.NoError(t, err)
	assert.Contains(t, out, "by user "+testAuthorID+"  ")
	assert.Contains(t, out, "Showing 1 note\n")
}

func TestNotesFiltersAndPages(t *testing.T) {
	fake, deps := newIssuesServer(t)
	fake.set(func(s *issuesServer) { s.next = "next-page" })

	out, _, err := execute(t, New(deps), nil, "notes", testIssueID, "--kind", "issue", "--limit", "2", "--cursor", "this-page")
	require.NoError(t, err)
	assert.Equal(t, url.Values{
		"project_id": {testProjectID}, "issue_id": {testIssueID}, "kind": {"issue"}, "limit": {"2"}, "cursor": {"this-page"},
	}, fake.last(t).query)
	assert.Contains(t, out, "volcano cloud issues notes "+testIssueID+" --limit 2 --cursor next-page --kind issue")
}

func TestNotesRefusesABadRequestBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"notes"}, "pass one issue ID, from 'volcano cloud issues list'"},
		{[]string{"notes", "checkout"}, `invalid issue ID "checkout": use an ID from 'volcano cloud issues list'`},
		{[]string{"notes", testIssueID, "--kind", "all"}, `invalid --kind "all": use issue or run`},
		{[]string{"notes", testIssueID, "--limit", "0"}, "--limit must be between 1 and 100"},
	} {
		fake, deps := newIssuesServer(t)

		_, _, err := execute(t, New(deps), nil, tc.args...)
		require.EqualError(t, err, tc.want, "%q", tc.args)
		assert.Empty(t, fake.sent())
	}
}

func TestNotesSaysWhenThereAreNone(t *testing.T) {
	fake, deps := newIssuesServer(t)
	fake.set(func(s *issuesServer) { s.notes = nil })

	out, _, err := execute(t, New(deps), nil, "notes", testIssueID)
	require.NoError(t, err)
	assert.Equal(t, "No notes\n", out)
}

// add prints one line, which says what LavaFlow does with the note.
func TestAddWritesANoteOnTheIssue(t *testing.T) {
	fake, deps := newIssuesServer(t)

	out, errOut, err := execute(t, New(deps), nil, "notes", "add", testIssueID, "--text", "Coupons without an expiry never expire.")
	require.NoError(t, err)
	sent := fake.last(t)
	assert.Equal(t, http.MethodPost, sent.method)
	assert.Equal(t, map[string]any{
		"project_id": testProjectID, "issue_id": testIssueID, "body": "Coupons without an expiry never expire.",
	}, sent.body)
	assert.Equal(t, "✓ Note "+testNoteID+" added. "+nextRunReads+"\n", out)
	assert.Empty(t, errOut)
}

// Text on stdin keeps its lines; the line break a pipe ends with is dropped.
func TestAddReadsTheNoteFromStdin(t *testing.T) {
	fake, deps := newIssuesServer(t)

	_, _, err := execute(t, New(deps), strings.NewReader("First line.\n\nThird line.\n"), "notes", "add", testIssueID)
	require.NoError(t, err)
	assert.Equal(t, "First line.\n\nThird line.", fake.last(t).body["body"])
}

func TestAddPutsANoteOnARun(t *testing.T) {
	fake, deps := newIssuesServer(t)

	out, _, err := execute(t, New(deps), nil, "notes", "add", testIssueID, "--run", testRunID, "--text", "Closed: wrong place.")
	require.NoError(t, err)
	assert.Equal(t, testRunID, fake.last(t).body["run_id"])
	assert.Equal(t, "✓ Note "+testNoteID+" added. Later LavaFlow runs don't read notes on a run.\n", out)
}

// --json keeps stdout JSON and says what LavaFlow does on stderr.
func TestAddJSONIsTheNote(t *testing.T) {
	_, deps := newIssuesServer(t)

	out, errOut, err := execute(t, New(deps), nil, "notes", "add", testIssueID, "--text", "a note", "--json")
	require.NoError(t, err)
	note := decodeJSON(t, out)
	assert.Equal(t, testNoteID, note["id"])
	assert.Equal(t, "a note", note["body"])
	assert.Equal(t, nextRunReads+"\n", errOut)
}

func TestAddRefusesANoteTheAPIWouldBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"empty", []string{"--text", ""}, "", "the note is empty: pass its text with --text, or on stdin"},
		{"blank", []string{"--text", " \n\t"}, "", "the note is empty: pass its text with --text, or on stdin"},
		{"empty stdin", nil, "\n", "the note is empty: pass its text with --text, or on stdin"},
		{"bad run", []string{"--text", "a note", "--run", "latest"}, "", `invalid --run "latest": use a run ID from 'volcano cloud lavaflow list'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, deps := newIssuesServer(t)

			args := append([]string{"notes", "add", testIssueID}, tc.args...)
			_, _, err := execute(t, New(deps), strings.NewReader(tc.stdin), args...)
			require.EqualError(t, err, tc.want)
			assert.Empty(t, fake.sent())
		})
	}
}

// Only the API can tell a note's size: it counts bytes once the text is
// normalized, which can shorten it.
func TestAddLeavesTheSizeToTheAPI(t *testing.T) {
	fake, deps := newIssuesServer(t)
	text := strings.Repeat("ｆｕｌｌｗｉｄｔｈ", 300)

	_, _, err := execute(t, New(deps), nil, "notes", "add", testIssueID, "--text", text)
	require.NoError(t, err)
	assert.Equal(t, text, fake.last(t).body["body"])
}

// A note on stdin past what any note can hold is refused before it is sent.
func TestAddRefusesAHugeNoteOnStdin(t *testing.T) {
	fake, deps := newIssuesServer(t)

	_, _, err := execute(t, New(deps), strings.NewReader(strings.Repeat("x", 64<<10+1)), "notes", "add", testIssueID)
	require.EqualError(t, err, "the note on stdin is over 64 KiB: a note holds at most 2,048 bytes")
	assert.Empty(t, fake.sent())
}

func TestEditReplacesANotesText(t *testing.T) {
	fake, deps := newIssuesServer(t)

	out, _, err := execute(t, New(deps), nil, "notes", "edit", testNoteID, "--text", "Only SAVE10 lacks an expiry.")
	require.NoError(t, err)
	sent := fake.last(t)
	assert.Equal(t, http.MethodPatch, sent.method)
	assert.Equal(t, "/issue-notes/"+testNoteID, sent.path)
	assert.Equal(t, url.Values{"project_id": {testProjectID}}, sent.query)
	assert.Equal(t, map[string]any{"body": "Only SAVE10 lacks an expiry."}, sent.body)
	assert.Equal(t, "✓ Note "+testNoteID+" saved, version 2. "+nextRunReads+"\n", out)
}

func TestDeleteAsksFirst(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		stdin   string
		deletes bool
	}{
		{"yes flag", []string{"--yes"}, "", true},
		{"answered yes", nil, "yes\n", true},
		{"answered no", nil, "n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, deps := newIssuesServer(t)

			out, _, err := execute(t, New(deps), strings.NewReader(tc.stdin), append([]string{"notes", "delete", testNoteID}, tc.args...)...)
			require.NoError(t, err)
			if !tc.deletes {
				assert.Empty(t, fake.sent())
				assert.Contains(t, out, "Delete cancelled.")
				return
			}
			sent := fake.last(t)
			assert.Equal(t, http.MethodDelete, sent.method)
			assert.Equal(t, "/issue-notes/"+testNoteID, sent.path)
			assert.Equal(t, url.Values{"project_id": {testProjectID}}, sent.query)
			assert.Contains(t, out, "✓ Note "+testNoteID+" deleted\n")
		})
	}
}

// A note is input to LavaFlow's next run, so a project access token can't
// write one. The CLI refuses before it asks or sends anything, with the exit
// code of a permission refused.
func TestWritesRefuseAProjectAccessTokenBeforeSending(t *testing.T) {
	for _, args := range [][]string{
		{"notes", "add", testIssueID, "--text", "a note"},
		{"notes", "edit", testNoteID, "--text", "a note"},
		{"notes", "delete", testNoteID},
	} {
		fake, deps := newIssuesServerWithToken(t, cliconfig.ProjectTokenPrefix+"secret")

		_, _, err := execute(t, New(deps), strings.NewReader("yes\n"), args...)
		require.ErrorIs(t, err, cliissues.ErrPersonWrites, "%q", args)
		require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired)
		require.EqualError(t, err, "writing issue notes needs a person's session: a project access token can't. "+
			"Sign in with 'volcano login' and run this again")
		assert.Empty(t, fake.sent())
	}

	_, deps := newIssuesServerWithToken(t, "")
	t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")
	_, _, err := execute(t, New(deps), nil, "notes", "add", testIssueID, "--text", "a note")
	require.EqualError(t, err, "writing issue notes needs a person's session: a project access token can't. "+
		"Unset VOLCANO_TOKEN and sign in with 'volcano login', then run this again")
}

// A project access token still reads notes.
func TestAProjectAccessTokenReadsNotes(t *testing.T) {
	_, deps := newIssuesServerWithToken(t, cliconfig.ProjectTokenPrefix+"secret")

	out, _, err := execute(t, New(deps), nil, "notes", testIssueID)
	require.NoError(t, err)
	assert.Contains(t, out, "Showing 2 notes")
}

// Each refusal says why in the CLI's words, keeping the API's refusal for the
// exit code. A text check names its rule and never quotes the note.
func TestWriteRefusalsSayWhy(t *testing.T) {
	for _, tc := range []struct {
		status  int
		code    string
		message string
		headers map[string]string
		want    string
	}{
		{400, "characters", "refused", nil, "the note wasn't saved (characters): it holds a control, format, private-use " +
			"or other invisible character. Rephrase it and send it again"},
		{400, "size", "refused", nil, "the note wasn't saved (size): it is over 2,048 bytes once normalized to Unicode " +
			"NFKC. Rephrase it and send it again"},
		{400, "credential", "refused", nil, "the note wasn't saved (credential): it holds what looks like a credential, " +
			"such as an API key or a token. Rephrase it and send it again"},
		{400, "secret", "refused", nil, "the note wasn't saved (secret): it holds one of the project's secret values. " +
			"Rephrase it and send it again"},
		{400, "sanitizer", "refused", nil, "the note wasn't saved (sanitizer): it holds text Volcano masks in logs. " +
			"Rephrase it and send it again"},
		{400, "link", "refused", nil, "the note wasn't saved (link): it holds a link or a line reference: a URL, a host " +
			"name with //, user info, a port, a path or a query, or a line such as cart.ts:42 or a stack frame. Name a " +
			"line in words, such as \"cart.ts, line 42\". Rephrase it and send it again"},
		{403, "person_required", "this platform token never expires; sign in with `volcano login`, or use a token " +
			"that expires, to write issue notes", nil, "the note wasn't saved: this platform token never expires; sign in " +
			"with `volcano login`, or use a token that expires, to write issue notes"},
		{403, "person_required", "", nil, "the note wasn't saved: only the project's owner writes issue notes, as a " +
			"person: signed in with 'volcano login', or with a platform token that expires"},
		{403, "note_author_required", "only the author", nil, "the note wasn't saved: only the note's author can edit " +
			"it, and only its author or the project's owner can delete it"},
		{403, "", "forbidden", nil, "the note wasn't saved: a read-only account or access token can't change notes"},
		{404, "feature_unavailable", "local mode can't write issue notes", nil, "writing issue notes isn't available " +
			"in this Volcano environment yet"},
		{409, "note_limit_reached", "full", nil, "the note wasn't saved: the issue or the project holds as many notes " +
			"as it can; delete notes to write more"},
		{429, "", "slow down", map[string]string{"Retry-After": "12"}, "the note wasn't saved: the project wrote 30 " +
			"notes or edits in the last minute. Try again in 12s"},
		{413, "", "Request body too large", nil, "the note wasn't saved (size): it is over 2,048 bytes once " +
			"normalized to Unicode NFKC. Rephrase it and send it again"},
		{404, "", "issue or run not found", nil, "failed to add a note to issue " + testIssueID + ": HTTP 404: issue or run not found"},
		{400, "", "body must hold text", nil, "failed to add a note to issue " + testIssueID + ": HTTP 400: body must hold text"},
	} {
		fake, deps := newIssuesServer(t)
		fake.set(func(s *issuesServer) {
			s.refuse = tc.status
			s.refusal = map[string]any{"error": tc.message}
			if tc.message == "" {
				s.refusal["error"] = http.StatusText(tc.status)
			}
			if tc.code != "" {
				s.refusal["code"] = tc.code
			}
			s.refusalHeaders = tc.headers
		})
		if tc.code == "person_required" && tc.message == "" {
			// A person_required without the API's words gets the CLI's.
			fake.set(func(s *issuesServer) { s.refusal["error"] = "" })
		}

		_, _, err := execute(t, New(deps), nil, "notes", "add", testIssueID, "--text", "Coupon codes ignore case.")
		require.EqualError(t, err, tc.want, "%d %s", tc.status, tc.code)
		assert.Equal(t, tc.status, api.Status(err), "the refusal keeps its status for the exit code")
		assert.NotContains(t, err.Error(), "Coupon", "a refusal never quotes the note")
	}
}

// A token in VOLCANO_TOKEN wins over a sign-in, so a person_required says so.
func TestAPersonRefusalNamesTheTokenInTheEnvironment(t *testing.T) {
	fake, deps := newIssuesServerWithToken(t, "")
	t.Setenv("VOLCANO_TOKEN", "vol_platform_token")
	fake.set(func(s *issuesServer) {
		s.refuse = http.StatusForbidden
		s.refusal = map[string]any{"error": "this platform token never expires", "code": "person_required"}
	})

	_, _, err := execute(t, New(deps), nil, "notes", "edit", testNoteID, "--text", "a note")
	require.EqualError(t, err, "the note wasn't saved: this platform token never expires. VOLCANO_TOKEN is set, and "+
		"its token is used instead of a 'volcano login' sign-in until you unset it")
}

func TestDeleteRefusalsSayTheNoteWasNotDeleted(t *testing.T) {
	fake, deps := newIssuesServer(t)
	fake.set(func(s *issuesServer) {
		s.refuse = http.StatusForbidden
		s.refusal = map[string]any{"error": "only the author", "code": "note_author_required"}
	})

	_, _, err := execute(t, New(deps), nil, "notes", "delete", testNoteID, "--yes")
	require.EqualError(t, err, "the note wasn't deleted: only the note's author can edit it, and only its author or "+
		"the project's owner can delete it")
}

func TestWritesSayWhenTheAPIHasNoNotes(t *testing.T) {
	fake, deps := newIssuesServer(t)
	fake.set(func(s *issuesServer) { s.routesMissing = true })

	_, _, err := execute(t, New(deps), nil, "notes", "add", testIssueID, "--text", "a note")
	require.ErrorIs(t, err, cliissues.ErrNotesUnavailable)
	require.EqualError(t, err, "issue notes aren't available on this Volcano API")
}

// Help on every write says that LavaFlow's next run reads notes, and lists
// the rules a note is checked against.
func TestWriteHelpSaysWhatLavaFlowReads(t *testing.T) {
	_, deps := newIssuesServer(t)

	add, _, err := execute(t, New(deps), nil, "notes", "add", "--help")
	require.NoError(t, err)
	assert.Contains(t, add, nextRunReads)
	for _, rule := range []string{"characters", "size", "credential", "secret", "sanitizer", "link"} {
		assert.Contains(t, add, "  "+rule+" ")
	}
	assert.NotContains(t, strings.ToLower(add), "autofix")

	edit, _, err := execute(t, New(deps), nil, "notes", "edit", "--help")
	require.NoError(t, err)
	assert.Contains(t, edit, "LavaFlow's next run on the issue\nreads the new text.")
}
