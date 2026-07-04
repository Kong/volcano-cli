package cloud

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	cloudProjectID  = "33333333-3333-4333-8333-333333333333"
	cloudBucketID   = "44444444-4444-4444-8444-444444444444"
	cloudObjectID   = "66666666-6666-4666-8666-666666666666"
	cloudFunctionID = "77777777-7777-4777-8777-777777777777"
)

func TestCloudStorageObjectCommandsUseCLIServiceKey(t *testing.T) {
	setCloudCommandTestHome(t)
	saveCloudCommandTestConfig(t)

	var serviceKeyListHits int
	var storageListAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+cloudProjectID+"/service-keys":
			serviceKeyListHits++
			assert.Equal(t, "Bearer platform-token", r.Header.Get("Authorization"))
			writeCloudCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					cloudServiceKeyPayload("11111111-1111-4111-8111-111111111111", "sk-storage"),
				},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/storage/uploads":
			storageListAuth = r.Header.Get("Authorization")
			writeCloudCommandJSON(t, w, http.StatusOK, map[string]any{
				"objects": []any{cloudObjectPayload("hello.txt")},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeCloudCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "storage", "object", "list", "uploads")
	require.NoError(t, err)
	assert.Contains(t, out, "hello.txt")
	assert.Equal(t, 1, serviceKeyListHits)
	assert.Equal(t, "Bearer sk-storage", storageListAuth)
}

func TestCloudFunctionInvokeUsesCLIServiceKey(t *testing.T) {
	setCloudCommandTestHome(t)
	saveCloudCommandTestConfig(t)

	var serviceKeyListHits int
	var invokeAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+cloudProjectID+"/service-keys":
			serviceKeyListHits++
			assert.Equal(t, "Bearer platform-token", r.Header.Get("Authorization"))
			writeCloudCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					cloudServiceKeyPayload("22222222-2222-4222-8222-222222222222", "sk-functions"),
				},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/functions/"+cloudFunctionID+"/invoke":
			invokeAuth = r.Header.Get("Authorization")
			writeCloudCommandJSON(t, w, http.StatusOK, map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeCloudCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "functions", "invoke", "--id", cloudFunctionID, "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, out)
	assert.Equal(t, 1, serviceKeyListHits)
	assert.Equal(t, "Bearer sk-functions", invokeAuth)
}

func executeCloudCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setCloudCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveCloudCommandTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "platform-token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   cloudProjectID,
			Name: "Gamma",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeCloudCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func cloudServiceKeyPayload(id, keyValue string) map[string]any {
	return map[string]any{
		"id":          id,
		"project_id":  cloudProjectID,
		"name":        "volcano-cli-data-plane",
		"key_prefix":  keyValue[:min(len(keyValue), 12)],
		"key_value":   keyValue,
		"permissions": []string{"*"},
		"created_at":  "2026-05-20T00:00:00Z",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}

func cloudObjectPayload(name string) map[string]any {
	return map[string]any{
		"id":         cloudObjectID,
		"bucket_id":  cloudBucketID,
		"name":       name,
		"size":       12,
		"mime_type":  "text/plain",
		"is_public":  false,
		"created_at": "2026-05-20T00:00:00Z",
		"updated_at": "2026-05-20T00:00:00Z",
	}
}
