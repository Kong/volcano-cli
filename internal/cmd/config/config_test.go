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
	configProjectID   = "55555555-5555-4555-8555-555555555555"
	configBucketID    = "66666666-6666-4666-8666-666666666666"
	configFunctionID  = "77777777-7777-4777-8777-777777777777"
	configPolicyID    = "88888888-8888-4888-8888-888888888888"
	storageBucketsURL = "/projects/" + configProjectID + "/storage/buckets"
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

func TestDeployCreatesBucketPolicyAndFlipsFunction(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)

	manifestPath := filepath.Join(dir, "volcano-config.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(`version: 1
buckets:
  - name: uploads
    file_size_limit: 2048
    allowed_mime_types:
      - image/png
    policies:
      - name: owner
        operation: select
        definition: "auth.uid() = owner_id"
functions:
  - name: hello
    public: true
`), 0o644))

	var createBucketBody map[string]any
	var createPolicyBody map[string]any
	var visibilityBody map[string]bool
	functionPublic := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == storageBucketsURL+"/uploads":
			http.NotFound(w, r)
		case r.Method == http.MethodPost && r.URL.Path == storageBucketsURL:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBucketBody))
			writeConfigCommandJSON(t, w, http.StatusCreated, map[string]any{
				"id":              configBucketID,
				"name":            "uploads",
				"project_id":      configProjectID,
				"file_size_limit": 2048,
				"created_at":      "2026-05-20T00:00:00Z",
				"updated_at":      "2026-05-20T00:00:00Z",
			})
		case r.Method == http.MethodGet && r.URL.Path == storageBucketsURL+"/uploads/policies":
			writeConfigCommandJSON(t, w, http.StatusOK, []any{})
		case r.Method == http.MethodPost && r.URL.Path == storageBucketsURL+"/uploads/policies":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createPolicyBody))
			writeConfigCommandJSON(t, w, http.StatusCreated, map[string]any{
				"id":         configPolicyID,
				"bucket_id":  configBucketID,
				"name":       "owner",
				"operation":  "SELECT",
				"definition": "auth.uid() = owner_id",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+configProjectID+"/functions":
			writeConfigCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{map[string]any{
					"id":               configFunctionID,
					"name":             "hello",
					"project_id":       configProjectID,
					"deployed_regions": []string{"aws-us-east-1"},
					"is_public":        functionPublic,
					"runtime":          "nodejs24.x",
					"status":           "active",
					"created_at":       "2026-05-20T00:00:00Z",
					"updated_at":       "2026-05-20T00:00:00Z",
				}},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/"+configProjectID+"/functions/"+configFunctionID:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&visibilityBody))
			functionPublic = visibilityBody["is_public"]
			writeConfigCommandJSON(t, w, http.StatusOK, map[string]any{
				"id":               configFunctionID,
				"name":             "hello",
				"project_id":       configProjectID,
				"deployed_regions": []string{"aws-us-east-1"},
				"is_public":        functionPublic,
				"runtime":          "nodejs24.x",
				"status":           "active",
				"created_at":       "2026-05-20T00:00:00Z",
				"updated_at":       "2026-05-20T00:00:00Z",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy")
	require.NoError(t, err)

	assert.Contains(t, out, "Configuration deployed from volcano-config.yaml")
	assert.Contains(t, out, "Buckets: 1 created, 0 updated, 0 unchanged")
	assert.Contains(t, out, "Policies: 1 created, 0 updated, 0 deleted, 0 unchanged")
	assert.Contains(t, out, "Functions: 1 updated, 0 unchanged")

	assert.Equal(t, "uploads", createBucketBody["name"])
	assert.EqualValues(t, 2048, createBucketBody["file_size_limit"])
	assert.Equal(t, "owner", createPolicyBody["name"])
	assert.Equal(t, "SELECT", createPolicyBody["operation"])
	assert.True(t, visibilityBody["is_public"])
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
	require.NoError(t, os.WriteFile(customPath, []byte(`version: 1
buckets:
  - name: uploads
`), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == storageBucketsURL+"/uploads":
			writeConfigCommandJSON(t, w, http.StatusOK, map[string]any{
				"id":         configBucketID,
				"name":       "uploads",
				"project_id": configProjectID,
				"created_at": "2026-05-20T00:00:00Z",
				"updated_at": "2026-05-20T00:00:00Z",
			})
		case r.Method == http.MethodGet && r.URL.Path == storageBucketsURL+"/uploads/policies":
			writeConfigCommandJSON(t, w, http.StatusOK, []any{})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeConfigCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "-f", filepath.Join("config", "custom.yaml"))
	require.NoError(t, err)
	assert.Contains(t, out, "Configuration deployed from custom.yaml")
	assert.Contains(t, out, "Buckets: 0 created, 0 updated, 1 unchanged")
}

func TestDeployInvalidManifest(t *testing.T) {
	dir := chdirToTemp(t)
	setConfigCommandTestHome(t)
	saveConfigCommandTestConfig(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano-config.yaml"), []byte("version: 1\n"), 0o644))

	out, err := executeConfigCommand(t, New(cliruntime.Deps{}), "deploy")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must include at least one bucket or function")
	assert.NotContains(t, out, "Configuration deployed from")
}
