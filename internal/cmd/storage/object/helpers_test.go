package object

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

const (
	storageProjectID = "33333333-3333-4333-8333-333333333333"
	storageBucketID  = "44444444-4444-4444-8444-444444444444"
	storageObjectID  = "66666666-6666-4666-8666-666666666666"
)

func executeObjectCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setObjectCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveObjectCommandTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   storageProjectID,
			Name: "Gamma",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeObjectJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func objectPayload(name string, size int64, isPublic bool) map[string]any {
	return map[string]any{
		"id":         storageObjectID,
		"bucket_id":  storageBucketID,
		"name":       name,
		"size":       size,
		"mime_type":  "text/plain",
		"is_public":  isPublic,
		"created_at": "2026-05-20T00:00:00Z",
		"updated_at": "2026-05-20T00:00:00Z",
	}
}
