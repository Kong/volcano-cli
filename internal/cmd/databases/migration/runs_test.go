package migration

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	testMigrationGrant = cliconfig.DelegatedGrantPrefix + "grant-secret"
	testRunID          = "44444444-4444-4444-8444-444444444444"
	testEarlierRunID   = "55555555-5555-4555-8555-555555555555"
	migrationRunsPath  = "/projects/" + migrationProjectID + "/databases/app/migration-runs"
)

// fakeMigrationRuns is a recording migration-runs API. Each GET of a run
// returns the next state from its list, and stays on the last.
type fakeMigrationRuns struct {
	t        *testing.T
	mu       sync.Mutex
	requests []string
	bodies   []map[string]any
	keys     []string
	create   []func(w http.ResponseWriter)
	states   map[string][]map[string]any
}

func (f *fakeMigrationRuns) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	assert.Equal(f.t, "Bearer "+testMigrationGrant, r.Header.Get("Authorization"))
	switch {
	case r.Method == http.MethodPost && r.URL.Path == migrationRunsPath:
		var body map[string]any
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		f.bodies = append(f.bodies, body)
		f.keys = append(f.keys, r.Header.Get("Idempotency-Key"))
		respond := f.create[0]
		if len(f.create) > 1 {
			f.create = f.create[1:]
		}
		respond(w)
	case r.Method == http.MethodGet && len(r.URL.Path) > len(migrationRunsPath)+1:
		id := r.URL.Path[len(migrationRunsPath)+1:]
		states := f.states[id]
		if len(states) == 0 {
			writeRunJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
			return
		}
		state := states[0]
		if len(states) > 1 {
			f.states[id] = states[1:]
		}
		writeRunJSON(w, http.StatusOK, state)
	default:
		writeRunJSON(w, http.StatusForbidden, map[string]any{
			"error": "delegated grants cannot be used on this endpoint", "code": "grant_route_not_allowed",
		})
	}
}

func writeRunJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func runState(id, status, phase string, extra map[string]any) map[string]any {
	state := map[string]any{
		"id": id, "status": status, "phase": phase, "source": "api", "warnings": []any{},
		"database":   map[string]any{"id": "66666666-6666-4666-8666-666666666666", "name": "app"},
		"created_at": "2026-11-11T00:00:00Z", "updated_at": "2026-11-11T00:00:00Z",
	}
	maps.Copy(state, extra)
	return state
}

func acceptRun(id string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		writeRunJSON(w, http.StatusAccepted, runState(id, "queued", "plan", nil))
	}
}

// setGrantMigrationTest makes a project with two migration files and the
// grant in VOLCANO_TOKEN, and polls without waiting.
func setGrantMigrationTest(t *testing.T) {
	t.Helper()
	setMigrationCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(filepath.Join("volcano", "migrations"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "002_second.sql"), []byte("CREATE TABLE second;"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "001_first.sql"), []byte("CREATE TABLE first;"), 0o644))
	saveMigrationCommandTestConfig(t)
	t.Setenv("VOLCANO_TOKEN", testMigrationGrant)
	previous := runPollInterval
	runPollInterval = 0
	t.Cleanup(func() { runPollInterval = previous })
}

func runGrantMigration(t *testing.T, fake *fakeMigrationRuns, args ...string) (string, error) {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	executor := func(context.Context, string, string) error {
		t.Error("a grant must never connect to the database directly")
		return nil
	}
	cmd := newWithExecutor(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, executor)
	out, err := executeMigrationCommand(t, cmd, args...)
	assert.NotContains(t, out, "grant-secret")
	if err != nil {
		assert.NotContains(t, err.Error(), "grant-secret")
	}
	for _, request := range fake.requests {
		assert.NotEqual(t, "GET /projects/"+migrationProjectID+"/databases/app", request, "a grant must never read the connection string")
	}
	return out, err
}

func TestMigrationUpUsesRunsUnderGrant(t *testing.T) {
	setGrantMigrationTest(t)
	fake := &fakeMigrationRuns{t: t, create: []func(http.ResponseWriter){acceptRun(testRunID)}, states: map[string][]map[string]any{
		testRunID: {
			runState(testRunID, "running", "rehearse", nil),
			runState(testRunID, "succeeded", "apply", map[string]any{
				"backup_name": "before-migration-1",
				"files": []any{
					map[string]any{"name": "001_first.sql", "outcome": "already_applied"},
					map[string]any{"name": "002_second.sql", "outcome": "applied"},
				},
			}),
		},
	}}

	out, err := runGrantMigration(t, fake, "up", "--all", "-d", "app")
	require.NoError(t, err)

	assert.Equal(t, []string{
		"POST " + migrationRunsPath,
		"GET " + migrationRunsPath + "/" + testRunID,
		"GET " + migrationRunsPath + "/" + testRunID,
	}, fake.requests)
	require.Len(t, fake.bodies, 1)
	assert.Equal(t, []any{
		map[string]any{"name": "001_first.sql", "sql": "CREATE TABLE first;"},
		map[string]any{"name": "002_second.sql", "sql": "CREATE TABLE second;"},
	}, fake.bodies[0]["files"])
	assert.NotEmpty(t, fake.keys[0], "Idempotency-Key")
	for _, want := range []string{
		"Started migration run " + testRunID,
		testRunID + ": rehearse",
		testRunID + ": apply",
		"001_first.sql: already_applied",
		"002_second.sql: applied",
		"Backup: before-migration-1",
		"Migrations deployed!",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "without tracking")
}

func TestMigrationUpUnderGrantSendsOneFile(t *testing.T) {
	setGrantMigrationTest(t)
	fake := &fakeMigrationRuns{t: t, create: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		writeRunJSON(w, http.StatusOK, runState(testRunID, "succeeded", "apply", nil))
	}}}

	_, err := runGrantMigration(t, fake, "up", "-f", "002_second", "-d", "app")
	require.NoError(t, err)
	assert.Equal(t, []any{map[string]any{"name": "002_second.sql", "sql": "CREATE TABLE second;"}}, fake.bodies[0]["files"])
	assert.Equal(t, []string{"POST " + migrationRunsPath}, fake.requests, "a 200 replay that already ended needs no read")
}

func TestMigrationUpUnderGrantReportsAFailedRun(t *testing.T) {
	setGrantMigrationTest(t)
	fake := &fakeMigrationRuns{t: t, create: []func(http.ResponseWriter){acceptRun(testRunID)}, states: map[string][]map[string]any{
		testRunID: {runState(testRunID, "failed", "rehearse", map[string]any{
			"error": map[string]any{"code": "migration_failed", "message": "002_second.sql failed at line 1 (SQLSTATE 42P07)"},
			"files": []any{
				map[string]any{"name": "001_first.sql", "outcome": "applied"},
				map[string]any{"name": "002_second.sql", "outcome": "failed", "error": map[string]any{
					"message": `relation "second" already exists`, "sqlstate": "42P07", "line": 1,
				}},
			},
		})},
	}}

	out, err := runGrantMigration(t, fake, "up", "--all", "-d", "app")
	require.Error(t, err)
	assert.Equal(t, "migration run "+testRunID+" failed in rehearse: migration_failed: 002_second.sql failed at line 1 (SQLSTATE 42P07)", err.Error())
	assert.Contains(t, out, `relation "second" already exists (line 1, SQLSTATE 42P07)`)
	assert.NotContains(t, err.Error(), "already exists", "the database's own message stays out of the error")
}

// Where migration runs are off, a grant fails and never falls back to the
// connection string.
func TestMigrationUpUnderGrantRefusesWhenRunsAreOff(t *testing.T) {
	setGrantMigrationTest(t)
	fake := &fakeMigrationRuns{t: t, create: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		writeRunJSON(w, http.StatusNotFound, map[string]any{"error": "migration runs are not available", "code": "feature_unavailable"})
	}}}

	_, err := runGrantMigration(t, fake, "up", "--all", "-d", "app")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "migration runs are not available on this Volcano API")
	assert.Contains(t, err.Error(), "nothing was applied")
	assert.Equal(t, []string{"POST " + migrationRunsPath}, fake.requests)
}

// A run an earlier command started is still going: wait for it, then start
// this command's run once.
func TestMigrationUpUnderGrantWaitsForARunInProgress(t *testing.T) {
	setGrantMigrationTest(t)
	fake := &fakeMigrationRuns{t: t, create: []func(http.ResponseWriter){
		func(w http.ResponseWriter) {
			writeRunJSON(w, http.StatusConflict, map[string]any{
				"error": "a migration run is in progress", "code": "migration_run_in_progress", "run_id": testEarlierRunID,
			})
		},
		acceptRun(testRunID),
	}, states: map[string][]map[string]any{
		testEarlierRunID: {
			runState(testEarlierRunID, "running", "apply", nil),
			runState(testEarlierRunID, "succeeded", "apply", nil),
		},
		testRunID: {runState(testRunID, "succeeded", "apply", nil)},
	}}

	out, err := runGrantMigration(t, fake, "up", "--all", "-d", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "Migration run "+testEarlierRunID+" is already running on app; waiting for it to end first.")
	assert.Equal(t, []string{
		"POST " + migrationRunsPath,
		"GET " + migrationRunsPath + "/" + testEarlierRunID,
		"GET " + migrationRunsPath + "/" + testEarlierRunID,
		"POST " + migrationRunsPath,
		"GET " + migrationRunsPath + "/" + testRunID,
	}, fake.requests)
	require.Len(t, fake.keys, 2)
	assert.Equal(t, fake.keys[0], fake.keys[1], "one command, one key")
}

// The grant expires while the run goes on: the command says the run carries on.
func TestMigrationUpUnderGrantStopsOnAnExpiredGrant(t *testing.T) {
	setGrantMigrationTest(t)
	fake := &fakeMigrationRuns{t: t, create: []func(http.ResponseWriter){acceptRun(testRunID)}, states: map[string][]map[string]any{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fake.mu.Lock()
			fake.requests = append(fake.requests, r.Method+" "+r.URL.Path)
			fake.mu.Unlock()
			writeRunJSON(w, http.StatusUnauthorized, map[string]any{"error": "invalid or expired token"})
			return
		}
		fake.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	cmd := newWithExecutor(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, nil)
	out, err := executeMigrationCommand(t, cmd, "up", "--all", "-d", "app")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stopped following migration run "+testRunID+", which carries on on the server")
	assert.Contains(t, out, "Started migration run "+testRunID)
	assert.NotContains(t, out, "grant-secret")
}

func TestMigrationUpUnderGrantRefusesAnUppercaseExtension(t *testing.T) {
	setGrantMigrationTest(t)
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "migrations", "003_third.SQL"), []byte("SELECT 1;"), 0o644))
	fake := &fakeMigrationRuns{t: t}

	_, err := runGrantMigration(t, fake, "up", "--all", "-d", "app")
	require.ErrorContains(t, err, "003_third.SQL: a migration run takes only names ending in lowercase .sql")
	assert.Empty(t, fake.requests)
}

// `deploy --with-migrations` applies through ApplyAll, which takes the same
// path under a grant.
func TestApplyAllUsesRunsUnderGrant(t *testing.T) {
	setGrantMigrationTest(t)
	fake := &fakeMigrationRuns{t: t, create: []func(http.ResponseWriter){acceptRun(testRunID)}, states: map[string][]map[string]any{
		testRunID: {runState(testRunID, "succeeded", "apply", nil)},
	}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	var out bytes.Buffer
	err := ApplyAll(t.Context(), cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, "app", &out)
	require.NoError(t, err)
	assert.Equal(t, []string{"POST " + migrationRunsPath, "GET " + migrationRunsPath + "/" + testRunID}, fake.requests)
}

// A spent budget keeps the API's error, so the CLI exits 6.
func TestMigrationUpUnderGrantKeepsTheBudgetRefusal(t *testing.T) {
	setGrantMigrationTest(t)
	fake := &fakeMigrationRuns{t: t, create: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		writeRunJSON(w, http.StatusTooManyRequests, map[string]any{
			"error": "the delegated grant has no mutations left; ask the app for a new grant", "code": "grant_budget_exhausted",
		})
	}}}

	_, err := runGrantMigration(t, fake, "up", "--all", "-d", "app")
	require.Error(t, err)
	assert.True(t, api.GrantBudgetExhausted(err), "%v", err)
}
