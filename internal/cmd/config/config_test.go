package config

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	configProjectID  = "55555555-5555-4555-8555-555555555555"
	projectConfigURL = "/projects/" + configProjectID + "/config"
)

func executeConfigCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setConfigCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveConfigCommandTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   configProjectID,
			Name: "Delta",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeConfigCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

// chdirToTemp moves the test into a tempdir for the duration of the test so
// volcano-config.yaml lookup is isolated from the developer's working tree.
func chdirToTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	resolved, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return resolved
}

func applyResultResponse() map[string]any {
	return map[string]any{
		"results": []any{
			map[string]any{"section": "variables", "name": "API_KEY", "action": "created"},
			map[string]any{"section": "variables", "name": "STALE", "action": "deleted"},
			map[string]any{"section": "realtime", "action": "updated", "notice": "disabling realtime drops active connections"},
			map[string]any{"section": "databases", "name": "appdb", "action": "unchanged"},
		},
		"skipped": []any{
			map[string]any{"type": "function", "name": "hello", "reason": "not deployed"},
		},
		"missing": []any{
			map[string]any{"type": "frontend", "name": "web"},
		},
		"summary": map[string]any{
			"created": 1, "updated": 1, "deleted": 1, "unchanged": 1,
			"errors": 0, "skipped": 1, "missing": 1,
		},
	}
}

func TestDeployUploadsManifestAndRendersReport(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)
	t.Setenv("DEPLOY_TEST_SECRET", "interpolated-value")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte(`version: 1
variables:
  - name: API_KEY
    value: ${DEPLOY_TEST_SECRET}
realtime:
  enabled: false
databases:
  - name: appdb
    region: aws-us-east-1
    pg_version: "16"
`), 0o644))

	var method, query string
	var uploaded map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != projectConfigURL {
			http.NotFound(w, r)
			return
		}
		method = r.Method
		query = r.URL.RawQuery
		require.NoError(t, json.NewDecoder(r.Body).Decode(&uploaded))
		writeConfigCommandJSON(t, w, http.StatusOK, applyResultResponse())
	}))
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy")
	require.NoError(t, err)

	assert.Equal(t, http.MethodPut, method)
	assert.Empty(t, query)
	assert.EqualValues(t, 1, uploaded["version"])
	variables, ok := uploaded["variables"].([]any)
	require.True(t, ok)
	variable, ok := variables[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "interpolated-value", variable["value"])
	realtime, ok := uploaded["realtime"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, false, realtime["enabled"])

	assert.Contains(t, out, "Configuration deployed from volcano-config.yaml")
	assert.Contains(t, out, "variables: 1 created, 1 deleted")
	assert.Contains(t, out, "realtime: 1 updated")
	assert.Contains(t, out, "databases: 1 unchanged")
	assert.Contains(t, out, "Summary: 1 created, 1 updated, 1 deleted, 1 unchanged")
	assert.Contains(t, out, "Note: realtime: disabling realtime drops active connections")
	assert.Contains(t, out, `Warning: function "hello" is declared in the manifest but not deployed`)
	assert.Contains(t, out, `Warning: frontend "web" exists but is not covered by your manifest`)
}

func TestDeployDryRunSendsQueryParamAndPrefixesOutput(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte("version: 1\nrealtime:\n  enabled: true\n"), 0o644))

	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		response := applyResultResponse()
		response["dry_run"] = true
		writeConfigCommandJSON(t, w, http.StatusOK, response)
	}))
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--dry-run")
	require.NoError(t, err)
	assert.Equal(t, "dry_run=true", query)
	assert.Contains(t, out, "Dry run: projected actions, nothing was applied.")
	assert.NotContains(t, out, "Configuration deployed from")
}

func TestDeployValidationFailureRendersErrorListAndExitsNonZero(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte("version: 1\nrealtime:\n  enabled: true\n"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeConfigCommandJSON(t, w, http.StatusUnprocessableEntity, map[string]any{
			"error": "configuration validation failed",
			"errors": []any{
				map[string]any{"section": "databases", "name": "appdb", "message": "tier changes are not supported via config"},
				map[string]any{"section": "functions.schedulers", "name": "nightly", "message": "invalid cron expression"},
			},
		})
	}))
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed with 2 error(s)")
	assert.Contains(t, out, `databases "appdb": tier changes are not supported via config`)
	assert.Contains(t, out, `functions.schedulers "nightly": invalid cron expression`)
	assert.NotContains(t, out, "Configuration deployed from")
}

func TestDeployApplyErrorsExitNonZero(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte("version: 1\nrealtime:\n  enabled: true\n"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeConfigCommandJSON(t, w, http.StatusOK, map[string]any{
			"results": []any{
				map[string]any{"section": "realtime", "action": "error", "error": "provider unavailable"},
			},
			"skipped": []any{},
			"missing": []any{},
			"summary": map[string]any{
				"created": 0, "updated": 0, "deleted": 0, "unchanged": 0,
				"errors": 1, "skipped": 0, "missing": 0,
			},
		})
	}))
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "1 configuration change(s) failed to apply")
	assert.Contains(t, out, "Error: realtime: provider unavailable")
}

func TestDeployMissingEnvVarFailsBeforeUpload(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte("version: 1\nvariables:\n  - name: A\n    value: ${DEFINITELY_NOT_SET_VAR}\n"), 0o644))

	requestSeen := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestSeen = true
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `environment variable "DEFINITELY_NOT_SET_VAR" is not set`)
	assert.False(t, requestSeen)
}

func TestDeployConcurrentApplyConflict(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte("version: 1\nrealtime:\n  enabled: true\n"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeConfigCommandJSON(t, w, http.StatusConflict, map[string]any{
			"error": "another config apply is already in progress for this project",
		})
	}))
	defer server.Close()

	_, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")
}

func TestDeployOldServerWithoutConfigEndpoint(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte("version: 1\nrealtime:\n  enabled: true\n"), 0o644))

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support declarative config apply")
	assert.Contains(t, err.Error(), "upgrade")
}

func TestDeployMissingManifest(t *testing.T) {
	chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	out, err := executeConfigCommand(t, New(cliruntime.Deps{}), "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no volcano-config.yaml file found")
	assert.NotContains(t, out, "Configuration deployed from")
}

func TestDeployExplicitFile(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	customPath := filepath.Join(dir, "config", "custom.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(customPath), 0o755))
	require.NoError(t, os.WriteFile(customPath, []byte("version: 1\nrealtime:\n  enabled: true\n"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeConfigCommandJSON(t, w, http.StatusOK, map[string]any{
			"results": []any{map[string]any{"section": "realtime", "action": "unchanged"}},
			"skipped": []any{},
			"missing": []any{},
			"summary": map[string]any{
				"created": 0, "updated": 0, "deleted": 0, "unchanged": 1,
				"errors": 0, "skipped": 0, "missing": 0,
			},
		})
	}))
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "-f", filepath.Join("config", "custom.yaml"))
	require.NoError(t, err)
	assert.Contains(t, out, "Configuration deployed from custom.yaml")
	assert.Contains(t, out, "realtime: 1 unchanged")
}

func TestDeployRejectedSchedulerRegions(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte(`version: 1
functions:
  - name: hello
    schedulers:
      - name: nightly
        cron: "0 3 * * *"
        regions: [aws-us-east-1]
`), 0o644))

	_, err := executeConfigCommand(t, New(cliruntime.Deps{}), "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "scheduler placement is managed by the server")
}

const pulledYAML = `# volcano-config.yaml (manifest version 1)
version: 1
variables:
  - name: API_KEY
    value: secret-value
`

// requirePulledYAMLVerbatim asserts byte-for-byte fidelity: pull must save
// the server-rendered manifest exactly, including comments and ordering.
func requirePulledYAMLVerbatim(t *testing.T, path string) {
	t.Helper()
	written, err := os.ReadFile(path)
	require.NoError(t, err)
	require.True(t, bytes.Equal([]byte(pulledYAML), written), "pulled manifest was not saved verbatim:\n%s", string(written))
}

func newPullTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != projectConfigURL {
			http.NotFound(w, r)
			return
		}
		assert.Equal(t, "yaml", r.URL.Query().Get("format"))
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(pulledYAML))
	}))
}

func TestPullWritesServerYAMLVerbatim(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	server := newPullTestServer(t)
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "pull")
	require.NoError(t, err)
	assert.Contains(t, out, "Configuration written to volcano-config.yaml")
	assert.Contains(t, out, "write-only secrets")

	requirePulledYAMLVerbatim(t, filepath.Join(dir, "volcano-config.yaml"))

	info, err := os.Stat(filepath.Join(dir, "volcano-config.yaml"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestPullPrefersVolcanoDirectory(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano"), 0o755))

	server := newPullTestServer(t)
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "pull")
	require.NoError(t, err)
	assert.Contains(t, out, filepath.Join("volcano", "volcano-config.yaml"))

	_, err = os.Stat(filepath.Join(dir, "volcano", "volcano-config.yaml"))
	require.NoError(t, err)
}

func TestPullRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte("version: 1\n"), 0o644))

	server := newPullTestServer(t)
	defer server.Close()

	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	_, err := executeConfigCommand(t, New(deps), "pull")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to overwrite existing file")
	assert.Contains(t, err.Error(), "--force")

	out, err := executeConfigCommand(t, New(deps), "pull", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "Configuration written to volcano-config.yaml")

	requirePulledYAMLVerbatim(t, filepath.Join(dir, "volcano-config.yaml"))
}

func TestPullExplicitFileCreatesDirectory(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	server := newPullTestServer(t)
	defer server.Close()

	target := filepath.Join("nested", "dir", "volcano-config.yaml")
	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "pull", "-f", target)
	require.NoError(t, err)
	assert.Contains(t, out, "Configuration written to "+target)

	requirePulledYAMLVerbatim(t, filepath.Join(dir, target))
}

func TestPullOldServerWithoutConfigEndpoint(t *testing.T) {
	chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	_, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "pull")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support declarative config export")
}
