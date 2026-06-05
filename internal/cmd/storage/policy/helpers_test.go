package policy

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
	storageProjectID    = "33333333-3333-4333-8333-333333333333"
	storageBucketID     = "44444444-4444-4444-8444-444444444444"
	storagePolicyID     = "55555555-5555-4555-8555-555555555555"
	storagePoliciesPath = "/projects/" + storageProjectID + "/storage/buckets/uploads/policies"
)

func executePolicyCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setPolicyCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func savePolicyCommandTestConfig(t *testing.T) {
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

func writePolicyJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func policyPayload(name, operation string) map[string]any {
	return map[string]any{
		"id":         storagePolicyID,
		"bucket_id":  storageBucketID,
		"name":       name,
		"operation":  operation,
		"definition": "auth.uid() = owner_id",
		"created_at": "2026-05-20T00:00:00Z",
		"updated_at": "2026-05-20T00:00:00Z",
	}
}
