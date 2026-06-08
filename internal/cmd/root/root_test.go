package root

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/update"
	"github.com/Kong/volcano-cli/internal/version"
)

func TestRootHelp(t *testing.T) {
	out, err := executeRootCommand(t, "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "volcano")
	assert.Contains(t, out, "databases")
	assert.Contains(t, out, "functions")
	assert.Contains(t, out, "init")
	assert.Contains(t, out, "local")
	assert.Contains(t, out, "projects")
	assert.Contains(t, out, "restart")
	assert.Contains(t, out, "start")
	assert.Contains(t, out, "status")
	assert.Contains(t, out, "stop")
	assert.Contains(t, out, "upgrade")
	assert.Contains(t, out, "variables")
	assert.NotContains(t, out, "migration")
}

func TestInitCommandPath(t *testing.T) {
	t.Chdir(t.TempDir())

	out, err := executeRootCommand(t, "init")
	require.NoError(t, err)
	assert.Contains(t, out, "Volcano project initialized.")
	assert.FileExists(t, filepath.Join("volcano", "functions", "hello.js"))
}

func TestDatabasesHelpIncludesMigration(t *testing.T) {
	out, err := executeRootCommand(t, "databases", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "migration")
}

func TestDatabaseMigrationsUpCommandPath(t *testing.T) {
	t.Chdir(t.TempDir())

	out, err := executeRootCommand(t, "databases", "migration", "up", "--all", "-d", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "No migration files found in volcano/migrations/")
}

func TestVersionFlag(t *testing.T) {
	out, err := executeRootCommand(t, "--version")
	require.NoError(t, err)
	assert.Equal(t, "volcano dev (commit none, built unknown)\n", out)
}

func TestVersionShortFlag(t *testing.T) {
	out, err := executeRootCommand(t, "-v")
	require.NoError(t, err)
	assert.Equal(t, "volcano dev (commit none, built unknown)\n", out)
}

func TestVersionSubcommand(t *testing.T) {
	out, err := executeRootCommand(t, "version")
	require.NoError(t, err)
	assert.Equal(t, "volcano dev (commit none, built unknown)\n", out)
}

func TestUpdateNoticePrintsForOutdatedVersion(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })
	t.Chdir(t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/releases/latest", r.URL.Path)
		writeRootJSON(t, w, update.Release{TagName: "v1.2.4"})
	}))
	defer server.Close()

	out, err := executeRootCommandWithDeps(t, cliruntime.Deps{
		HTTPClient:         server.Client(),
		UpdateGitHubAPIURL: server.URL,
	}, "init")
	require.NoError(t, err)
	assert.Contains(t, out, "A newer Volcano CLI version is available: v1.2.4 (current v1.2.3). Run `volcano upgrade` to upgrade.")
	assert.Contains(t, out, "Volcano project initialized.")
}

func TestUpdateNoticeSkipsVersionCommand(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeRootJSON(t, w, update.Release{TagName: "v1.2.4"})
	}))
	defer server.Close()

	out, err := executeRootCommandWithDeps(t, cliruntime.Deps{
		HTTPClient:         server.Client(),
		UpdateGitHubAPIURL: server.URL,
	}, "version")
	require.NoError(t, err)
	assert.Equal(t, "volcano v1.2.3 (commit none, built unknown)\n", out)
	assert.Zero(t, requests)
}

func TestUpdateNoticeSkipsImplicitRootHelp(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeRootJSON(t, w, update.Release{TagName: "v1.2.4"})
	}))
	defer server.Close()

	out, err := executeRootCommandWithDeps(t, cliruntime.Deps{
		HTTPClient:         server.Client(),
		UpdateGitHubAPIURL: server.URL,
	})
	require.NoError(t, err)
	assert.Contains(t, out, "Volcano CLI")
	assert.NotContains(t, out, "A newer Volcano CLI version is available")
	assert.Zero(t, requests)
}

func executeRootCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return executeRootCommandWithDeps(t, cliruntime.Deps{}, args...)
}

func executeRootCommandWithDeps(t *testing.T, deps cliruntime.Deps, args ...string) (string, error) {
	t.Helper()
	cmd := New(deps)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func writeRootJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
