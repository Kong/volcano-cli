package migration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/confirm"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const baselinePath = "/projects/" + migrationProjectID + "/databases/app/migrations/baseline"

// baselineServer answers the baseline route with respond and fails any other
// request, the database read that holds the connection string included.
func baselineServer(t *testing.T, respond func(w http.ResponseWriter, files []any)) (*httptest.Server, *[]string) {
	t.Helper()
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		assert.Equal(t, "Bearer "+testMigrationGrant, r.Header.Get("Authorization"))
		if r.Method != http.MethodPost || r.URL.Path != baselinePath {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body map[string][]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		respond(w, body["files"])
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func runBaselineCommand(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	cmd := newWithExecutor(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, nil)
	out, err := executeMigrationCommand(t, cmd, append([]string{"baseline"}, args...)...)
	assert.NotContains(t, out, "grant-secret")
	return out, err
}

// Under a grant, baseline records the files the caller names through
// Hosting's baseline route, and never reads the connection string.
func TestBaselineUnderGrantRecordsTheNamedFiles(t *testing.T) {
	setGrantMigrationTest(t)
	var sent []any
	server, requests := baselineServer(t, func(w http.ResponseWriter, files []any) {
		sent = files
		writeRunJSON(w, http.StatusOK, map[string]any{"recorded": []string{"002_second.sql"}})
	})

	out, err := runBaselineCommand(t, server, "-d", "app", "-f", "002_second", "--yes")
	require.NoError(t, err)
	assert.Equal(t, []string{"POST " + baselinePath}, *requests)
	assert.Equal(t, []any{map[string]any{"name": "002_second.sql", "sql": "CREATE TABLE second;"}}, sent)
	assert.Contains(t, out, "Recorded 1 file(s) as applied in \"app\" without running them:\n  - 002_second.sql\n")
}

func TestBaselineAllAndJSON(t *testing.T) {
	setGrantMigrationTest(t)
	var sent []any
	server, _ := baselineServer(t, func(w http.ResponseWriter, files []any) {
		sent = files
		writeRunJSON(w, http.StatusOK, map[string]any{"recorded": []string{}})
	})

	out, err := runBaselineCommand(t, server, "-d", "app", "--all", "--yes", "--json")
	require.NoError(t, err)
	assert.Len(t, sent, 2)
	assert.JSONEq(t, `{"recorded":[]}`, out)
}

// No person answers a worker's command, so it has to pass --yes.
func TestBaselineWithoutYesRefusesWhenNoOneCanAnswer(t *testing.T) {
	setGrantMigrationTest(t)
	t.Setenv("VOLCANO_NONINTERACTIVE", "1")
	server, requests := baselineServer(t, func(w http.ResponseWriter, _ []any) {
		writeRunJSON(w, http.StatusOK, map[string]any{"recorded": []string{}})
	})

	_, err := runBaselineCommand(t, server, "-d", "app", "--all")
	require.ErrorIs(t, err, confirm.ErrCannotPrompt)
	assert.Empty(t, *requests)
}

func TestBaselineKeepsTheAPIRefusal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   map[string]any
		code   string
		want   string
	}{
		{"checksum mismatch", http.StatusConflict, map[string]any{"error": "001_first.sql is recorded with another checksum", "code": "checksum_mismatch"}, "checksum_mismatch", "001_first.sql is recorded with another checksum"},
		{"budget", http.StatusTooManyRequests, map[string]any{"error": "no mutations left", "code": "grant_budget_exhausted"}, "grant_budget_exhausted", "no mutations left"},
		{"not on the grant table", http.StatusForbidden, map[string]any{"error": "delegated grants cannot be used on this endpoint", "code": "grant_route_not_allowed"}, "grant_route_not_allowed", "cannot be used"},
		{"runs off", http.StatusNotFound, map[string]any{"error": "migration runs are not available", "code": "feature_unavailable"}, "feature_unavailable", "no migration history to record a baseline in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGrantMigrationTest(t)
			server, _ := baselineServer(t, func(w http.ResponseWriter, _ []any) { writeRunJSON(w, tc.status, tc.body) })

			_, err := runBaselineCommand(t, server, "-d", "app", "--all", "--yes")
			require.Error(t, err)
			assert.Equal(t, tc.status, api.Status(err))
			assert.Equal(t, tc.code, api.Code(err))
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestBaselineRefusesAnUnknownFile(t *testing.T) {
	setGrantMigrationTest(t)
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "notes.txt"), []byte("x"), 0o644))
	server, requests := baselineServer(t, func(w http.ResponseWriter, _ []any) {})

	_, err := runBaselineCommand(t, server, "-d", "app", "-f", "009_missing", "--yes")
	require.ErrorContains(t, err, `migration "009_missing" not found`)
	assert.Empty(t, *requests)
}
