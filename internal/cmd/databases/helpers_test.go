package databases

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

const databaseProjectID = "22222222-2222-4222-8222-222222222222"

func executeDatabaseCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setDatabaseCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveDatabaseCommandTestConfig(t *testing.T, cfg *cliconfig.Config) {
	t.Helper()
	require.NoError(t, cfg.Save())
}

func writeDatabaseCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func databaseCommandPayload(id, projectID, name string) map[string]any {
	return map[string]any{
		"connection_string": "postgres://example",
		"created_at":        time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		"database_type":     "volcano-db-s",
		"id":                id,
		"name":              name,
		"pg_version":        "16",
		"project_id":        projectID,
		"region":            "aws-us-east-1",
		"status":            "active",
		"updated_at":        time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339),
	}
}
