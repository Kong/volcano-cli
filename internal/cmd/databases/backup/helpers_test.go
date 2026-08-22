package backup

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	backupProjectID  = "33333333-3333-4333-8333-333333333333"
	backupDatabaseID = "44444444-4444-4444-8444-444444444444"
	restoreID        = "66666666-6666-4666-8666-666666666666"
	databasePath     = "/projects/" + backupProjectID + "/databases/app"
	backupsPath      = databasePath + "/backups"
	backupPath       = backupsPath + "/nightly"
	schedulePath     = databasePath + "/backup-schedule"
	restoresPath     = databasePath + "/restores"
)

func executeBackupCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString(""))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setBackupCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveBackupCommandTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   backupProjectID,
			Name: "Gamma",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeBackupJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func decodeBody(t *testing.T, r *http.Request, target any) {
	t.Helper()
	require.NoError(t, json.NewDecoder(r.Body).Decode(target))
}

func backupPayload(name, source string) map[string]any {
	return map[string]any{
		"name":       name,
		"source":     source,
		"created_at": "2026-05-20T00:00:00Z",
	}
}

func restorePayload(kind, status string) map[string]any {
	return map[string]any{
		"id":          restoreID,
		"database_id": backupDatabaseID,
		"project_id":  backupProjectID,
		"kind":        kind,
		"status":      status,
		"created_at":  "2026-05-20T00:00:00Z",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}

func newTestCommand(server *httptest.Server) *cobra.Command {
	return New(testDeps(server))
}

func newTestRestoreCommand(server *httptest.Server) *cobra.Command {
	return NewRestore(testDeps(server))
}

func newTestScheduleCommand(server *httptest.Server) *cobra.Command {
	return NewSchedule(testDeps(server))
}

func testDeps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
}

// refusingServer fails the test if the API is called at all, for the cases a
// usage error or a declined prompt must stop before the request.
func refusingServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
	}))
}
