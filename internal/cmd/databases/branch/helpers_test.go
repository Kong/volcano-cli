package branch

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

const (
	branchProjectID  = "33333333-3333-4333-8333-333333333333"
	branchDatabaseID = "44444444-4444-4444-8444-444444444444"
	branchID         = "55555555-5555-4555-8555-555555555555"
	branchesPath     = "/projects/" + branchProjectID + "/databases/app/branches"
	branchPath       = branchesPath + "/feature_x"
)

func executeBranchCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setBranchCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveBranchCommandTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   branchProjectID,
			Name: "Gamma",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeBranchJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func branchPayload(name, status string) map[string]any {
	return map[string]any{
		"id":          branchID,
		"database_id": branchDatabaseID,
		"project_id":  branchProjectID,
		"name":        name,
		"status":      status,
		"ttl_seconds": 604800,
		"expires_at":  time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339),
		"created_at":  "2026-05-20T00:00:00Z",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}
