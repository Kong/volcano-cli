package sandboxes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

const filesListPath = "/sandbox-sessions/" + testSession + "/files/list"

// listServer answers each listing request with the next of pages and records
// every request body it received.
func listServer(t *testing.T, pages ...string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, filesListPath, r.URL.Path)
		var body map[string]any
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&body)) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		bodies = append(bodies, body)
		if len(bodies) > len(pages) {
			t.Errorf("unexpected request %d: %v", len(bodies), body)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(pages[len(bodies)-1]))
	}))
	t.Cleanup(server.Close)
	return server, &bodies
}

func runFilesList(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	cmd := New(testDeps(server))
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"files", "list"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestFilesListFollowsEveryPageWithTheSameRequest(t *testing.T) {
	t.Parallel()
	server, bodies := listServer(t,
		`{"entries":[{"path":"src/a","type":"directory","size":0,"mode":493},{"path":"src/a/b.py","type":"file","size":3,"mode":420,"sha256":"`+strings.Repeat("a", 64)+`"}],"next_cursor":"page-2"}`,
		`{"entries":[{"path":"src/c","type":"symlink","size":0,"mode":511}]}`,
	)
	out, err := runFilesList(t, server, testSession, "src", "--recursive=false", "--sha256", "--limit", "2", "--json")
	require.NoError(t, err)

	first := map[string]any{"path": "src", "recursive": false, "include_sha256": true, "limit": float64(2)}
	second := map[string]any{"path": "src", "recursive": false, "include_sha256": true, "limit": float64(2), "cursor": "page-2"}
	assert.Equal(t, []map[string]any{first, second}, *bodies)

	assert.Equal(t, 1, strings.Count(out, "\n"), "compact JSON on one line")
	var entries []apiclient.SandboxFileEntry
	require.NoError(t, json.Unmarshal([]byte(out), &entries))
	sum := strings.Repeat("a", 64)
	assert.Equal(t, []apiclient.SandboxFileEntry{
		{Path: "src/a", Type: apiclient.Directory, Mode: 0o755},
		{Path: "src/a/b.py", Type: apiclient.File, Size: 3, Mode: 0o644, Sha256: &sum},
		{Path: "src/c", Type: apiclient.Symlink, Mode: 0o777},
	}, entries)
}

func TestFilesListLeavesDefaultsToTheAPI(t *testing.T) {
	t.Parallel()
	server, bodies := listServer(t, `{"entries":[]}`)
	out, err := runFilesList(t, server, testSession, "--json")
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{}}, *bodies, "no path, recursion, digest, or limit unless asked")
	assert.Equal(t, "[]\n", out)

	server, bodies = listServer(t, `{"entries":[]}`)
	out, err = runFilesList(t, server, testSession, "--recursive")
	require.NoError(t, err)
	assert.Equal(t, []map[string]any{{"recursive": true}}, *bodies)
	assert.Equal(t, "No entries\n", out)
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
	out, err := runFilesList(t, server, testSession)
	require.NoError(t, err)
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
	var entries []apiclient.SandboxFileEntry
	require.NoError(t, json.Unmarshal([]byte(out), &entries))
	require.Len(t, entries, 1)
	assert.Equal(t, path, entries[0].Path)
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
		"workspace changed": {
			status: http.StatusConflict,
			body:   `{"error":"a file changed while it was hashed","code":"sandbox_workspace_changed"}`,
			args:   []string{"--sha256"},
			want:   "a file changed while the listing hashed it; run the command again",
		},
		"not a directory": {
			status: http.StatusBadRequest,
			body:   `{"error":"path is not a directory","code":"sandbox_path_not_directory"}`,
			args:   []string{"src/main.py"},
			want:   `"src/main.py" is not a directory in the session's workspace: it is missing, is a file, or passes through a symlink`,
		},
		"unsupported name": {
			status: http.StatusBadRequest,
			body:   `{"error":"the listing reached a name that is not UTF-8","code":"sandbox_path_unsupported"}`,
			want:   "HTTP 400: the listing reached a name that is not UTF-8",
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
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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

func TestFilesListRefusesAnInvalidSessionIDBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	server, bodies := listServer(t)
	_, err := runFilesList(t, server, "not-a-session")
	require.Error(t, err)
	assert.Empty(t, *bodies)

	_, err = runFilesList(t, server, testSession, "src", "extra")
	require.ErrorContains(t, err, "accepts between 1 and 2 arg(s)")
	assert.Empty(t, *bodies)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}
