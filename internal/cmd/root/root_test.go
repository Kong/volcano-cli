package root

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestRootHelp(t *testing.T) {
	out, err := executeRootCommand(t, "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "volcano")
	assert.Contains(t, out, "databases")
	assert.Contains(t, out, "functions")
	assert.Contains(t, out, "init")
	assert.Contains(t, out, "cloud")
	assert.Contains(t, out, "projects")
	assert.Contains(t, out, "restart")
	assert.Contains(t, out, "start")
	assert.Contains(t, out, "status")
	assert.Contains(t, out, "stop")
	assert.Contains(t, out, "upgrade")
	assert.Contains(t, out, "variables")
	assert.NotContains(t, out, "frontends")
	assert.NotContains(t, out, "\n  local ")
}

func TestCloudHelpIncludesCloudResources(t *testing.T) {
	out, err := executeRootCommand(t, "cloud", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "databases")
	assert.Contains(t, out, "frontends")
	assert.Contains(t, out, "functions")
	assert.Contains(t, out, "storage")
	assert.Contains(t, out, "variables")
}

func TestInitCommandPath(t *testing.T) {
	t.Chdir(t.TempDir())

	out, err := executeRootCommand(t, "init")
	require.NoError(t, err)
	assert.Contains(t, out, "Volcano project initialized.")
	assert.FileExists(t, filepath.Join("volcano", "README.md"))
	assert.NoFileExists(t, filepath.Join("volcano", "functions", "hello.js"))
}

func TestDatabasesHelpIncludesMigration(t *testing.T) {
	out, err := executeRootCommand(t, "databases", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "migration")
}

func TestDatabaseMigrationsUpCommandPath(t *testing.T) {
	t.Chdir(t.TempDir())

	out, err := executeRootCommand(t, "cloud", "databases", "migration", "up", "--all", "-d", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "No migration files found in volcano/migrations/")
}

func TestDirectFunctionCommandUsesLocalMetadata(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "cloud-token")
	t.Setenv("VOLCANO_PROJECT_ID", "99999999-9999-4999-8999-999999999999")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	var cloudHits int
	cloudServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		cloudHits++
		writeRootCommandJSON(t, w, http.StatusOK, map[string]any{
			"runtimes": []any{rootFunctionRuntimePayload("cloud-runtime")},
		})
	}))
	defer cloudServer.Close()
	t.Setenv("VOLCANO_API_URL", cloudServer.URL)

	var localHits int
	localServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer local-token", r.Header.Get("Authorization"))
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/functions/runtimes", r.URL.Path)
		localHits++
		writeRootCommandJSON(t, w, http.StatusOK, map[string]any{
			"runtimes": []any{rootFunctionRuntimePayload("local-runtime")},
		})
	}))
	defer localServer.Close()

	deps := cliruntime.Deps{
		HTTPClient: localServer.Client(),
		LocalCommandRunner: localmode.CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
			assert.Equal(t, "docker", name)
			assert.Equal(t, []string{"exec", "volcano-server", "/app/volcano-hosting", "local", "info", "--format", "json"}, args)
			return []byte(rootLocalInfoJSON(localServer.URL)), nil
		}),
	}

	out, err := executeRootCommandWithDeps(t, deps, "functions", "runtimes")
	require.NoError(t, err)
	assert.Contains(t, out, "local-runtime")
	assert.NotContains(t, out, "cloud-runtime")
	assert.Equal(t, 1, localHits)
	assert.Equal(t, 0, cloudHits)
}

func TestCloudFunctionCommandUsesCloudAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/functions/runtimes", r.URL.Path)
		hits++
		writeRootCommandJSON(t, w, http.StatusOK, map[string]any{
			"runtimes": []any{rootFunctionRuntimePayload("cloud-runtime")},
		})
	}))
	defer server.Close()

	out, err := executeRootCommandWithDeps(t, cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
	}, "cloud", "functions", "runtimes")
	require.NoError(t, err)
	assert.Contains(t, out, "cloud-runtime")
	assert.Equal(t, 1, hits)
}

func TestCloudFunctionHelpUsesCloudPaths(t *testing.T) {
	out, err := executeRootCommand(t, "cloud", "functions", "deploy", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "volcano cloud functions deploy --all")
	assert.NotContains(t, out, "volcano functions deploy --all")
}

func TestCloudFrontendHelpUsesCloudPaths(t *testing.T) {
	out, err := executeRootCommand(t, "cloud", "frontends", "deploy", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "volcano cloud frontends deploy --name web --path ./apps/web")
	assert.NotContains(t, out, "volcano frontends deploy --name web")
}

func TestCloudFunctionPaginationUsesCloudPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "cloud-token")
	t.Setenv("VOLCANO_PROJECT_ID", "22222222-2222-4222-8222-222222222222")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/projects/22222222-2222-4222-8222-222222222222/functions", r.URL.Path)
		writeRootCommandJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{rootFunctionPayload("33333333-3333-4333-8333-333333333333", "hello")},
			"has_more": true,
			"page":     1,
			"limit":    100,
			"total":    2,
		})
	}))
	defer server.Close()
	t.Setenv("VOLCANO_API_URL", server.URL)

	out, err := executeRootCommandWithDeps(t, cliruntime.Deps{HTTPClient: server.Client()}, "cloud", "functions", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "Next page: volcano cloud functions list --page 2 --limit 100")
	assert.NotContains(t, out, "Next page: volcano functions list")
}

func TestDeprecatedLocalAliasIsHiddenAndStillWorks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/functions/runtimes", r.URL.Path)
		writeRootCommandJSON(t, w, http.StatusOK, map[string]any{
			"runtimes": []any{rootFunctionRuntimePayload("local-runtime")},
		})
	}))
	defer server.Close()

	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		LocalCommandRunner: localmode.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			return []byte(rootLocalInfoJSON(server.URL)), nil
		}),
	}

	out, err := executeRootCommandWithDeps(t, deps, "local", "functions", "runtimes")
	require.NoError(t, err)
	assert.Contains(t, out, `warning: "volcano local ..." is deprecated`)
	assert.Contains(t, out, "local-runtime")
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

func writeRootCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, "%s\n", mustRootCommandJSON(t, value))
}

func mustRootCommandJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func rootLocalInfoJSON(apiURL string) string {
	return fmt.Sprintf(`{
		"api_url": %q,
		"project_id": "22222222-2222-4222-8222-222222222222",
		"project_name": "local-dev",
		"user_id": "local-user",
		"user_token": "local-token",
		"service_key": "local-service-key",
		"default_database_name": "app",
		"default_database_region": "metadata-region",
		"default_database_postgres_version": "17",
		"database_url": "postgres://volcano:volcano@localhost:8002/app"
	}`, apiURL)
}

func rootFunctionRuntimePayload(name string) map[string]any {
	return map[string]any{
		"name":     name,
		"language": "nodejs",
		"default":  true,
		"deployment": map[string]any{
			"file_extensions":      []string{".js"},
			"entrypoint":           "index.js",
			"handler":              "handler",
			"dependency_manifests": []string{"package.json"},
		},
	}
}

func rootFunctionPayload(id, name string) map[string]any {
	return map[string]any{
		"created_at":       "2026-05-20T00:00:00Z",
		"deployed_regions": []string{"aws-us-east-1"},
		"handler":          "handler",
		"id":               id,
		"invoke_url":       "https://" + id + ".functions.volcano.dev/",
		"is_public":        true,
		"name":             name,
		"project_id":       "22222222-2222-4222-8222-222222222222",
		"runtime":          "nodejs24.x",
		"status":           "active",
		"updated_at":       "2026-05-20T00:00:00Z",
	}
}
