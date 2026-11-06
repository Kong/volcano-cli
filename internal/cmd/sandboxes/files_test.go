package sandboxes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

const (
	filesListPath = "/sandbox-session-files"
	sessionPath   = "/sandbox-sessions/" + testSession
	// sessionProject owns testSession. It is not the selected project, so a
	// listing that names the selected project instead fails the tests.
	sessionProject = "44444444-4444-4444-8444-444444444444"
	sessionBody    = `{"id":"` + testSession + `","project_id":"` + sessionProject + `","sandbox_id":null,"state":"running","desired_state":"running","region":"us-east-1","memory_mb":1024,"created_at":"2026-11-04T00:00:00Z","expires_at":null}`
)

// listQuery is the query a listing of testSession sends, with extra
// parameters added.
func listQuery(extra url.Values) url.Values {
	query := url.Values{"project_id": {sessionProject}, "session_id": {testSession}}
	maps.Copy(query, extra)
	return query
}

// listServer reads testSession as a session of sessionProject, answers each
// listing request with the next of pages, and records every listing query.
func listServer(t *testing.T, pages ...string) (*httptest.Server, *[]url.Values) {
	t.Helper()
	var queries []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case sessionPath:
			_, _ = w.Write([]byte(sessionBody))
			return
		case filesListPath:
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		queries = append(queries, r.URL.Query())
		if len(queries) > len(pages) {
			t.Errorf("unexpected request %d: %v", len(queries), r.URL.Query())
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(pages[len(queries)-1]))
	}))
	t.Cleanup(server.Close)
	return server, &queries
}

func runFilesList(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	out, _, err := runFilesListOutputs(t, server, args...)
	return out, err
}

// runFilesListOutputs runs files list and returns its stdout and stderr.
func runFilesListOutputs(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	cmd := New(testDeps(server))
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(append([]string{"files", "list"}, args...))
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

// listPage is a listing page of testSession in sessionProject; rest holds
// further top-level fields, each with a leading comma.
func listPage(entries, rest string) string {
	return `{"project_id":"` + sessionProject + `","session_id":"` + testSession + `","entries":[` + entries + `]` + rest + `}`
}

func TestFilesListFollowsEveryPageWithTheSameRequest(t *testing.T) {
	t.Parallel()
	server, queries := listServer(t,
		`{"project_id":"`+sessionProject+`","session_id":"`+testSession+`","entries":[{"path":"src/a","type":"directory","size":0,"mode":493},{"path":"src/a/b.py","type":"file","size":3,"mode":420,"sha256":"`+strings.Repeat("a", 64)+`"}],"next_cursor":"page-2"}`,
		`{"project_id":"`+sessionProject+`","session_id":"`+testSession+`","entries":[{"path":"src/c","type":"symlink","size":0,"mode":511}]}`,
	)
	out, err := runFilesList(t, server, testSession, "src", "--recursive=false", "--sha256", "--limit", "2", "--json")
	require.NoError(t, err)

	first := listQuery(url.Values{"path": {"src"}, "recursive": {"false"}, "include_sha256": {"true"}, "limit": {"2"}})
	second := listQuery(url.Values{"path": {"src"}, "recursive": {"false"}, "include_sha256": {"true"}, "limit": {"2"}, "cursor": {"page-2"}})
	assert.Equal(t, []url.Values{first, second}, *queries, "the session's project, not the selected one, on every page")

	assert.Equal(t, 1, strings.Count(out, "\n"), "compact JSON on one line")
	var listing apiclient.SandboxFileListResult
	require.NoError(t, json.Unmarshal([]byte(out), &listing))
	sum := strings.Repeat("a", 64)
	assert.Equal(t, apiclient.SandboxFileListResult{
		ProjectId: uuid.MustParse(sessionProject),
		SessionId: uuid.MustParse(testSession),
		Entries: []apiclient.SandboxFileEntry{
			{Path: "src/a", Type: apiclient.Directory, Mode: 0o755},
			{Path: "src/a/b.py", Type: apiclient.File, Size: 3, Mode: 0o644, Sha256: &sum},
			{Path: "src/c", Type: apiclient.Symlink, Mode: 0o777},
		},
	}, listing, "every page's entries, and no cursor")
}

func TestFilesListLeavesDefaultsToTheAPI(t *testing.T) {
	t.Parallel()
	server, queries := listServer(t, listPage("", ""))
	out, err := runFilesList(t, server, testSession, "--json")
	require.NoError(t, err)
	assert.Equal(t, []url.Values{listQuery(nil)}, *queries, "no path, recursion, digest, or limit unless asked")
	assert.JSONEq(t, `{"entries":[],"project_id":"`+sessionProject+`","session_id":"`+testSession+`"}`, out, "an empty listing is an empty entries array")

	server, queries = listServer(t, `{"entries":[]}`)
	out, err = runFilesList(t, server, testSession, "--recursive")
	require.NoError(t, err)
	assert.Equal(t, []url.Values{listQuery(url.Values{"recursive": {"true"}})}, *queries)
	assert.Equal(t, "No entries\n", out)
}

func TestFilesListSaysWhyAFileHasNoDigest(t *testing.T) {
	t.Parallel()
	sum := strings.Repeat("0123456789abcdef", 4)
	page := `{"entries":[` +
		`{"path":"big.bin","type":"file","size":300000000,"mode":420,"sha256_skipped":"budget"},` +
		`{"path":"hashed.txt","type":"file","size":3,"mode":420,"sha256":"` + sum + `"},` +
		`{"path":"linked.txt","type":"file","size":3,"mode":420,"sha256_skipped":"unreadable"},` +
		`{"path":"log.txt","type":"file","size":3,"mode":420,"sha256_skipped":"changing"},` +
		`{"path":"new.txt","type":"file","size":3,"mode":420,"sha256_skipped":"new\u001b[31m"},` +
		`{"path":"pkg","type":"directory","size":0,"mode":493}]}`

	server, _ := listServer(t, page)
	out, err := runFilesList(t, server, testSession, "--sha256")
	require.NoError(t, err)
	digest := func(value string) string { return fmt.Sprintf("%-64s", value) }
	assert.Equal(t, []string{
		"file       0644     300000000  " + digest("skipped: budget") + "  big.bin",
		"file       0644             3  " + sum + "  hashed.txt",
		"file       0644             3  " + digest("skipped: unreadable") + "  linked.txt",
		"file       0644             3  " + digest("skipped: changing") + "  log.txt",
		"file       0644             3  " + digest(`skipped: "new\x1b[31m"`) + "  new.txt",
		"directory  0755             -  " + digest("-") + "  pkg",
	}, strings.Split(out, "\n")[2:8])

	server, _ = listServer(t, page)
	out, err = runFilesList(t, server, testSession, "--sha256", "--json")
	require.NoError(t, err)
	var listing apiclient.SandboxFileListResult
	require.NoError(t, json.Unmarshal([]byte(out), &listing))
	entries := listing.Entries
	require.Len(t, entries, 6)
	skipped := func(reason apiclient.SandboxFileEntrySha256Skipped) *apiclient.SandboxFileEntrySha256Skipped {
		return &reason
	}
	assert.Equal(t, skipped(apiclient.Budget), entries[0].Sha256Skipped)
	assert.Nil(t, entries[1].Sha256Skipped)
	assert.Equal(t, &sum, entries[1].Sha256)
	assert.Equal(t, skipped(apiclient.Unreadable), entries[2].Sha256Skipped)
	assert.Equal(t, skipped(apiclient.Changing), entries[3].Sha256Skipped)
	assert.Contains(t, out, `"sha256_skipped":"changing"`)
	assert.Contains(t, out, `"sha256_skipped":"new\u001b[31m"`, "the JSON escapes an unknown reason's controls")
}

func TestFilesListPrintsATableWithPathLast(t *testing.T) {
	t.Parallel()
	sum := strings.Repeat("0123456789abcdef", 4)
	page := `{"entries":[` +
		`{"path":"src","type":"directory","size":0,"mode":493},` +
		`{"path":"src/main.py","type":"file","size":15,"mode":420,"sha256":"` + sum + `"},` +
		`{"path":"src/big.bin","type":"file","size":300000000,"mode":3565},` +
		`{"path":"src/link","type":"symlink","size":0,"mode":511},` +
		`{"path":"src/\u001b[31mred\tname","type":"file","size":1,"mode":384}]}`

	server, _ := listServer(t, page)
	out, errOut, err := runFilesListOutputs(t, server, testSession)
	require.NoError(t, err)
	assert.Empty(t, errOut, "a complete listing warns of nothing")
	assert.Equal(t, `Type       Mode          Size  Path
-----------------------------------
directory  0755             -  src
file       0644            15  src/main.py
file       6755     300000000  src/big.bin
symlink    0777             -  src/link
file       0600             1  "src/\x1b[31mred\tname"

Total: 5 entries
`, out)

	server, _ = listServer(t, page)
	out, err = runFilesList(t, server, testSession, "--sha256")
	require.NoError(t, err)
	lines := strings.Split(out, "\n")
	assert.Equal(t, "Type       Mode          Size  SHA-256                                                           Path", lines[0])
	assert.Equal(t, "file       0644            15  "+sum+"  src/main.py", lines[3])
	assert.Equal(t, "file       6755     300000000  -                                                                 src/big.bin", lines[4])
}

// incompletePage lists directories the listing could not enter or could not
// fully carry, and skipped one name directly in the listed directory.
var incompletePage = listPage(
	`{"path":"src","type":"directory","size":0,"mode":493},`+
		`{"path":"src/data","type":"directory","size":0,"mode":493,"unsupported_names":2},`+
		`{"path":"src/data/ok.txt","type":"file","size":3,"mode":420},`+
		`{"path":"src/locked","type":"directory","size":0,"mode":0,"unreadable":true},`+
		`{"path":"src/z","type":"directory","size":0,"mode":448,"unsupported_names":1},`+
		`{"path":"vault","type":"directory","size":0,"mode":256,"unreadable":true}`,
	`,"unsupported_names":1`)

func TestFilesListMarksWhatItCouldNotList(t *testing.T) {
	t.Parallel()
	server, _ := listServer(t, incompletePage)
	out, errOut, err := runFilesListOutputs(t, server, testSession)
	require.NoError(t, err, "an incomplete listing still succeeds")
	assert.Equal(t, `Type       Mode          Size  Path
-----------------------------------
directory  0755             -  src
directory  0755    incomplete  src/data
file       0644             3  src/data/ok.txt
directory  0000    unreadable  src/locked
directory  0700    incomplete  src/z
directory  0400    unreadable  vault

Total: 6 entries
`, out)
	assert.Equal(t, "\n"+`Warning: The listing is incomplete.
  2 directories marked unreadable were not entered: the session user lacks read or execute permission on them.
  4 names were skipped, with everything below them, because they are not UTF-8 or their paths would pass 4096 bytes (1 in the listed directory, 3 in 2 directories marked incomplete).
`, errOut)

	server, _ = listServer(t, incompletePage)
	out, _, err = runFilesListOutputs(t, server, testSession, "--sha256")
	require.NoError(t, err)
	assert.Equal(t, "directory  0000    unreadable  "+fmt.Sprintf("%-64s", "-")+"  src/locked", strings.Split(out, "\n")[5])
}

func TestFilesListWarnsOfOneSkippedNameOrDirectory(t *testing.T) {
	t.Parallel()
	server, _ := listServer(t, listPage(`{"path":"locked","type":"directory","size":0,"mode":0,"unreadable":true}`, `,"unsupported_names":1`))
	_, errOut, err := runFilesListOutputs(t, server, testSession)
	require.NoError(t, err)
	assert.Equal(t, "\n"+`Warning: The listing is incomplete.
  1 directory marked unreadable was not entered: the session user lacks read or execute permission on it.
  1 name was skipped, with everything below it, because it is not UTF-8 or its path would pass 4096 bytes (1 in the listed directory).
`, errOut)

	server, _ = listServer(t, listPage(`{"path":"odd","type":"directory","size":0,"mode":493,"unsupported_names":3}`, ""))
	_, errOut, err = runFilesListOutputs(t, server, testSession)
	require.NoError(t, err)
	assert.Equal(t, "\n"+`Warning: The listing is incomplete.
  3 names were skipped, with everything below them, because they are not UTF-8 or their paths would pass 4096 bytes (3 in 1 directory marked incomplete).
`, errOut)
}

func TestFilesListWarnsWhenItListsNothingButSkippedNames(t *testing.T) {
	t.Parallel()
	server, _ := listServer(t, listPage("", `,"unsupported_names":2`))
	out, errOut, err := runFilesListOutputs(t, server, testSession, "odd")
	require.NoError(t, err)
	assert.Equal(t, "No entries\n", out)
	assert.Contains(t, errOut, "\nWarning: The listing is incomplete.\n")
	assert.Contains(t, errOut, "2 names were skipped, with everything below them, because they are not UTF-8 or their paths would pass 4096 bytes (2 in the listed directory).")
}

func TestFilesListJSONPassesTheListingThrough(t *testing.T) {
	t.Parallel()
	server, _ := listServer(t, incompletePage)
	out, errOut, err := runFilesListOutputs(t, server, testSession, "--json")
	require.NoError(t, err)
	assert.JSONEq(t, incompletePage, out, "the API's fields as it sent them")
	assert.Empty(t, errOut, "the JSON carries the markers; no warning")
}

func TestFilesListKeepsTheLargestSkippedCountAcrossPages(t *testing.T) {
	t.Parallel()
	server, _ := listServer(t,
		listPage(`{"path":"a","type":"file","size":0,"mode":420}`, `,"unsupported_names":2,"next_cursor":"page-2"`),
		listPage(`{"path":"b","type":"file","size":0,"mode":420}`, `,"unsupported_names":3,"next_cursor":"page-3"`),
		listPage(`{"path":"c","type":"file","size":0,"mode":420}`, ""),
	)
	out, err := runFilesList(t, server, testSession, "--json")
	require.NoError(t, err)
	var listing apiclient.SandboxFileListResult
	require.NoError(t, json.Unmarshal([]byte(out), &listing))
	require.NotNil(t, listing.UnsupportedNames, "a later page without the count keeps an earlier one")
	assert.Equal(t, 3, *listing.UnsupportedNames)
	assert.Len(t, listing.Entries, 3)
	assert.Nil(t, listing.NextCursor)
}

func TestFilesListQuotesOnlyPathsThatNeedIt(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]string{
		"src/main.py":        "src/main.py",
		"docs/read me.md":    "docs/read me.md",
		"résumé/ünïcode.txt": "résumé/ünïcode.txt",
		"a\nb":               `"a\nb"`,
		`quote"d`:            `"quote\"d"`,
		`back\slash`:         `"back\\slash"`,
		"bidi\u202etxt.exe":  `"bidi\u202etxt.exe"`,
		"c1\u009bcontrol":    `"c1\u009bcontrol"`,
	} {
		server, _ := listServer(t, `{"entries":[{"path":`+mustJSON(t, path)+`,"type":"file","size":0,"mode":420}]}`)
		out, err := runFilesList(t, server, testSession)
		require.NoError(t, err)
		assert.Equal(t, "file       0644             0  "+want, strings.Split(out, "\n")[2], path)
	}
}

// The server names an entry's type and digest as well as its path. The table
// quotes any of them that holds a control.
func TestFilesListQuotesTheTypeAndDigest(t *testing.T) {
	t.Parallel()
	page := `{"entries":[{"path":"a","type":"file\u001b[2J","size":0,"mode":420,"sha256":"\u001b]8;;x\u0007"}]}`
	row := func(columns ...string) string { return strings.Join(columns, "  ") }

	server, _ := listServer(t, page)
	out, err := runFilesList(t, server, testSession)
	require.NoError(t, err)
	assert.Equal(t, row(`"file\x1b[2J"`, "0644", fmt.Sprintf("%12s", "-"), "a"), strings.Split(out, "\n")[2])
	assert.NotContains(t, out, "\x1b")

	server, _ = listServer(t, page)
	out, err = runFilesList(t, server, testSession, "--sha256")
	require.NoError(t, err)
	assert.Equal(t, row(`"file\x1b[2J"`, "0644", fmt.Sprintf("%12s", "-"), fmt.Sprintf("%-64s", `"\x1b]8;;x\a"`), "a"), strings.Split(out, "\n")[2])
	assert.NotContains(t, out, "\x1b")
}

func TestFilesListJSONEscapesEveryControl(t *testing.T) {
	t.Parallel()
	path := "a\u001bb\u007fc\u009bd"
	server, _ := listServer(t, `{"entries":[{"path":`+mustJSON(t, path)+`,"type":"file","size":0,"mode":420}]}`)
	out, err := runFilesList(t, server, testSession, "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `a\u001bb\u007fc\u009bd`)
	assert.NotContains(t, out, "\u001b")
	assert.NotContains(t, out, "\u007f")
	assert.NotContains(t, out, "\u009b")
	var listing apiclient.SandboxFileListResult
	require.NoError(t, json.Unmarshal([]byte(out), &listing))
	require.Len(t, listing.Entries, 1)
	assert.Equal(t, path, listing.Entries[0].Path)
}

// A listing that never ends stops after maxListPages pages.
func TestFilesListStopsAfterItsLastPage(t *testing.T) {
	t.Parallel()
	pages := make([]string, maxListPages+1)
	for i := range pages {
		pages[i] = fmt.Sprintf(`{"entries":[{"path":"f%06d","type":"file","size":0,"mode":420}],"next_cursor":"c%d"}`, i, i)
	}
	server, queries := listServer(t, pages...)
	out, err := runFilesList(t, server, testSession, "--json")
	require.ErrorContains(t, err, "the file listing did not end after 1000 pages")
	assert.Len(t, *queries, maxListPages)
	assert.Empty(t, out, "a stopped listing prints no partial result")
}

func TestFilesListStopsWhenTheCursorDoesNotMove(t *testing.T) {
	t.Parallel()
	server, bodies := listServer(t,
		`{"entries":[{"path":"a","type":"file","size":0,"mode":420}],"next_cursor":"same"}`,
		`{"entries":[{"path":"b","type":"file","size":0,"mode":420}],"next_cursor":"same"}`,
	)
	out, err := runFilesList(t, server, testSession, "--json")
	require.ErrorContains(t, err, "same file listing page twice")
	assert.Len(t, *bodies, 2)
	assert.Empty(t, out, "a failed listing prints no partial result")
}

func TestFilesListExplainsRefusals(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		status int
		body   string
		args   []string
		prefix string
		want   string
	}{
		"image outdated": {
			status: http.StatusConflict,
			body:   `{"error":"this session's image predates file listing","code":"sandbox_image_outdated"}`,
			want:   "this session's image predates file listing: start a new session with volcano sandboxes run; for a custom template, redeploy it with volcano sandboxes templates deploy first",
		},
		"image outdated in the cloud": {
			status: http.StatusConflict,
			body:   `{"error":"this session's image predates file listing","code":"sandbox_image_outdated"}`,
			prefix: "volcano cloud",
			want:   "start a new session with volcano cloud sandboxes run; for a custom template, redeploy it with volcano cloud sandboxes templates deploy first",
		},
		"not a directory": {
			status: http.StatusBadRequest,
			body:   `{"error":"path is not a directory","code":"sandbox_path_not_directory"}`,
			args:   []string{"src/main.py"},
			want:   `"src/main.py" is not a directory in the session's workspace: it is missing, is a file, or passes through a symlink`,
		},
		"unreadable": {
			status: http.StatusBadRequest,
			body:   `{"error":"the session user cannot list the path","code":"sandbox_path_unreadable"}`,
			args:   []string{"src/locked"},
			want:   `"src/locked" can't be listed: the session user lacks read or execute permission on it, or execute permission on a directory above it`,
		},
		"unreadable root": {
			status: http.StatusBadRequest,
			body:   `{"error":"the session user cannot list the path","code":"sandbox_path_unreadable"}`,
			want:   "the workspace root can't be listed",
		},
		"missing permission": {
			status: http.StatusForbidden,
			body:   `{"error":"sandbox access denied"}`,
			want:   "this credential can't list the session's files: it needs the sandboxes.files.read permission",
		},
		"sandboxes off": {
			status: http.StatusNotFound,
			body:   `{"error":"sandboxes are not available","code":"feature_unavailable"}`,
			want:   "HTTP 404: sandboxes are not available",
		},
		"older API": {
			status: http.StatusNotFound,
			body:   "404 page not found\n",
			want:   "this Volcano API cannot list Sandbox files yet",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == sessionPath {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(sessionBody))
					return
				}
				assert.Equal(t, filesListPath, r.URL.Path)
				if strings.HasPrefix(tc.body, "{") {
					w.Header().Set("Content-Type", "application/json")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			deps := testDeps(server)
			deps.CommandPathPrefix = tc.prefix
			cmd := New(deps)
			cmd.SilenceErrors = true
			cmd.SilenceUsage = true
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(append([]string{"files", "list", testSession}, tc.args...))
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Equal(t, tc.status, api.Status(err), "the refusal still sets the exit code")
			assert.Empty(t, out.String())
		})
	}
}

func TestFilesListStopsWhenTheSessionCannotBeRead(t *testing.T) {
	t.Parallel()
	var listed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sessionPath {
			listed.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"sandbox session not found"}`))
	}))
	t.Cleanup(server.Close)
	out, err := runFilesList(t, server, testSession)
	require.ErrorContains(t, err, "sandbox session not found")
	assert.Equal(t, http.StatusNotFound, api.Status(err))
	assert.False(t, listed.Load(), "no listing without the session's project")
	assert.Empty(t, out)
}

func TestFilesListRefusesAnInvalidSessionIDBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	server, queries := listServer(t)
	_, err := runFilesList(t, server, "not-a-session")
	require.Error(t, err)
	assert.Empty(t, *queries)

	_, err = runFilesList(t, server, testSession, "src", "extra")
	require.ErrorContains(t, err, "accepts between 1 and 2 arg(s)")
	assert.Empty(t, *queries)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
