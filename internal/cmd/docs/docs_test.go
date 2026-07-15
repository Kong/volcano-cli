package docs

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// fakeDocsServer serves the minimal GitHub API surface docs sync needs.
func fakeDocsServer(t *testing.T, files map[string]string) *httptest.Server {
	t.Helper()
	const repo = "Kong/volcano-cli"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/repos/" + repo
		switch {
		case strings.HasPrefix(r.URL.Path, base+"/commits/"):
			_ = json.NewEncoder(w).Encode(map[string]string{"sha": "deadbeefcafebabe"})
		case strings.HasPrefix(r.URL.Path, base+"/git/trees/"):
			type entry struct {
				Path string `json:"path"`
				Type string `json:"type"`
				SHA  string `json:"sha"`
				Size int64  `json:"size"`
			}
			out := struct {
				Tree      []entry `json:"tree"`
				Truncated bool    `json:"truncated"`
			}{}
			for p, c := range files {
				out.Tree = append(out.Tree, entry{Path: "docs/" + p, Type: "blob", SHA: p, Size: int64(len(c))})
			}
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, base+"/contents/"):
			full := strings.TrimPrefix(r.URL.Path, base+"/contents/docs/")
			content, ok := files[full]
			if !ok {
				http.Error(w, "nf", http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(content))
		default:
			http.Error(w, "nf", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testDeps(t *testing.T, srv *httptest.Server) cliruntime.Deps {
	t.Helper()
	return cliruntime.Deps{
		HTTPClient:       srv.Client(),
		DocsGitHubAPIURL: srv.URL,
		DocsCacheDir:     t.TempDir(),
		ConfigLoader:     func() (*cliconfig.Config, error) { return cliconfig.Default(), nil },
	}
}

func execDocs(t *testing.T, deps cliruntime.Deps, args ...string) (string, string, error) {
	t.Helper()
	cmd := New(deps)
	var out, errBuf bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	return out.String(), errBuf.String(), err
}

func TestDocsSearchJSONEnvelope(t *testing.T) {
	srv := fakeDocsServer(t, map[string]string{
		"authentication/keys.md": "# Keys\n\n## Service keys\nThe service_role key bypasses RLS.",
		"storage/buckets.md":     "# Buckets\n\n## Create\nCreate a storage bucket.",
	})
	deps := testDeps(t, srv)

	out, _, err := execDocs(t, deps, "search", "service key", "--json")
	require.NoError(t, err)

	var env envelope
	require.NoError(t, json.Unmarshal([]byte(out), &env))
	assert.Equal(t, 1, env.SchemaVersion)
	assert.Equal(t, "search", env.Command)
	assert.Equal(t, "github", env.Source.Provider)
	assert.Equal(t, "Kong/volcano-cli", env.Source.Repository)
	assert.Equal(t, "deadbeefcafebabe", env.Source.ResolvedCommit)
	require.NotNil(t, env.Cache)
	assert.False(t, env.Cache.Stale)

	results, ok := env.Data.([]any)
	require.True(t, ok)
	require.NotEmpty(t, results)
	first := results[0].(map[string]any)
	assert.Contains(t, first["path"], "authentication/keys.md")
}

func TestDocsGetJSON(t *testing.T) {
	srv := fakeDocsServer(t, map[string]string{
		"authentication/keys.md": "# Keys\n\n## Service keys\nThe service_role key bypasses RLS.",
	})
	deps := testDeps(t, srv)

	out, _, err := execDocs(t, deps, "get", "authentication/keys.md#service-keys", "--json")
	require.NoError(t, err)
	var env envelope
	require.NoError(t, json.Unmarshal([]byte(out), &env))
	assert.Equal(t, "get", env.Command)
	data := env.Data.(map[string]any)
	assert.Equal(t, "service-keys", data["anchor"])
	assert.Contains(t, data["content"], "service_role")
}

func TestDocsSearchOfflineMissingCacheJSONError(t *testing.T) {
	srv := fakeDocsServer(t, map[string]string{"a.md": "# A"})
	deps := testDeps(t, srv)

	out, _, err := execDocs(t, deps, "search", "anything", "--json", "--offline")
	require.Error(t, err)
	var env envelope
	require.NoError(t, json.Unmarshal([]byte(out), &env))
	require.NotNil(t, env.Error)
	assert.Equal(t, "DOCS_CACHE_MISSING", env.Error.Code)
}

func TestDocsListHuman(t *testing.T) {
	srv := fakeDocsServer(t, map[string]string{
		"authentication/keys.md": "# Keys\ncontent",
		"storage/buckets.md":     "# Buckets\ncontent",
	})
	deps := testDeps(t, srv)

	out, _, err := execDocs(t, deps, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "authentication/keys.md")
	assert.Contains(t, out, "storage/buckets.md")
	assert.Contains(t, out, "document(s)")
}

func TestDocsSyncHuman(t *testing.T) {
	srv := fakeDocsServer(t, map[string]string{"a.md": "# A", "b.md": "# B"})
	deps := testDeps(t, srv)

	out, _, err := execDocs(t, deps, "sync")
	require.NoError(t, err)
	assert.Contains(t, out, "Synced docs at commit deadbeef")
	assert.Contains(t, out, "added 2")
}

func TestDocsBareShowsHelp(t *testing.T) {
	srv := fakeDocsServer(t, map[string]string{"a.md": "# A"})
	deps := testDeps(t, srv)

	out, _, err := execDocs(t, deps)
	require.NoError(t, err)
	assert.Contains(t, out, "fetches the Volcano documentation corpus")
	assert.Contains(t, out, "sync")
	assert.Contains(t, out, "search")
}
