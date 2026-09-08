package durable

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

const (
	durableProjectID  = "22222222-2222-4222-8222-222222222222"
	durableFunctionID = "55555555-5555-4555-8555-555555555555"
	durableExecutionA = "66666666-6666-4666-8666-666666666666"
)

func executeDurableCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString("y\n"))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setDurableCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   durableProjectID,
			Name: "Beta",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeDurableCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func durableFunctionPayload(name string, isPublic bool) map[string]any {
	return map[string]any{
		"id":               durableFunctionID,
		"project_id":       durableProjectID,
		"name":             name,
		"runtime":          "nodejs24.x",
		"handler":          "handler",
		"kind":             "durable",
		"status":           "active",
		"is_public":        isPublic,
		"deployed_regions": []string{"aws-us-east-1"},
		"durable": map[string]any{
			"execution_timeout_seconds": 3600,
			"retention_days":            30,
		},
		"created_at": "2026-05-20T00:00:00Z",
		"updated_at": "2026-05-20T00:00:00Z",
	}
}

func durableExecutionPayload(id, name, status string) map[string]any {
	return map[string]any{
		"id":                  id,
		"durable_function_id": durableFunctionID,
		"project_id":          durableProjectID,
		"name":                name,
		"status":              status,
		"region":              "aws-us-east-1",
		"created_at":          "2026-05-20T00:00:00Z",
		"updated_at":          "2026-05-20T00:00:00Z",
	}
}

// writeDurableRuntimesResponse answers the runtime catalog the deploy path
// fetches before it scans sources.
func writeDurableRuntimesResponse(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	if r.Method != http.MethodGet || r.URL.Path != "/functions/runtimes" {
		return false
	}
	writeDurableCommandJSON(t, w, http.StatusOK, map[string]any{
		"runtimes": []any{
			map[string]any{
				"name":            "nodejs24.x",
				"language":        "javascript",
				"default":         true,
				"durable_capable": true,
				"deployment": map[string]any{
					"file_extensions":      []string{".js", ".mjs"},
					"entrypoint":           "index.js",
					"handler":              "handler",
					"dependency_manifests": []string{"package.json"},
				},
			},
			map[string]any{
				"name":            "python3.12",
				"language":        "python",
				"default":         true,
				"durable_capable": false,
				"deployment": map[string]any{
					"file_extensions":      []string{".py"},
					"entrypoint":           "main.py",
					"handler":              "handler",
					"dependency_manifests": []string{"requirements.txt"},
				},
			},
		},
	})
	return true
}

func writeDurableProjectFile(t *testing.T, path, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
}

func decodeJSONBody(r *http.Request, into any) error {
	return json.NewDecoder(r.Body).Decode(into)
}
