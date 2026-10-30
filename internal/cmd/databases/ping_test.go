package databases

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/dataplane"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const pingServiceKey = "sk-ping-test-value"

// pingAPI lists and creates the project's service keys and answers connection
// tests, recording the credential each test carried.
type pingAPI struct {
	t    *testing.T
	keys []any
	// status, when set, is the answer to a connection test.
	status  int
	message string

	mu         sync.Mutex
	pings      []string
	bearers    []string
	listedKeys int
	created    []map[string]any
}

func (a *pingAPI) serve() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+databaseProjectID+"/service-keys":
			a.mu.Lock()
			a.listedKeys++
			a.mu.Unlock()
			respondDatabaseJSON(a.t, w, http.StatusOK, map[string]any{
				"data": a.keys, "has_more": false, "page": 1, "limit": 100, "total": len(a.keys),
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+databaseProjectID+"/service-keys":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				a.t.Errorf("decode create body: %v", err)
			}
			a.mu.Lock()
			a.created = append(a.created, body)
			a.mu.Unlock()
			respondDatabaseJSON(a.t, w, http.StatusCreated, map[string]any{
				"id": "44444444-4444-4444-8444-444444444444", "name": body["name"], "key_prefix": "sk-created-k",
				"key_value": "sk-created-key-value", "permissions": body["permissions"],
			})
		case r.Method == http.MethodPost && (r.URL.Path == "/databases/app/query/ping" ||
			r.URL.Path == "/databases/app/branches/preview/query/ping"):
			a.mu.Lock()
			a.pings = append(a.pings, r.URL.Path)
			a.bearers = append(a.bearers, r.Header.Get("Authorization"))
			a.mu.Unlock()
			if a.status != 0 {
				respondDatabaseJSON(a.t, w, a.status, map[string]any{"error": a.message})
				return
			}
			respondDatabaseJSON(a.t, w, http.StatusOK, map[string]any{"data": []any{map[string]any{"?column?": 1}}, "count": 1})
		default:
			http.NotFound(w, r)
		}
	}))
	a.t.Cleanup(server.Close)
	return server
}

// sent returns the paths tested, the credentials sent, and how often the
// service keys were listed.
func (a *pingAPI) sent() ([]string, []string, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.pings...), append([]string(nil), a.bearers...), a.listedKeys
}

func newPingAPI(t *testing.T) *pingAPI {
	t.Helper()
	return &pingAPI{t: t, keys: []any{
		map[string]any{
			"id": "55555555-5555-4555-8555-555555555555", "name": "backend", "key_prefix": "sk-full-acce",
			"key_value": "sk-full-access-key", "permissions": []string{"*"},
		},
		map[string]any{
			"id": "33333333-3333-4333-8333-333333333333", "name": dataplane.CLIServiceKeyName, "key_prefix": "sk-ping-test",
			"key_value": pingServiceKey, "permissions": []string{"functions.invoke"},
		},
	}}
}

func runPingCommand(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	return runPingCommandAs(t, server, "platform-token", args...)
}

func runPingCommandAs(t *testing.T, server *httptest.Server, token string, args ...string) (string, error) {
	t.Helper()
	setDatabaseCommandTestHome(t)
	saveDatabaseCommandTestConfig(t, &cliconfig.Config{
		UserToken:      token,
		CurrentProject: &cliconfig.ProjectConfig{ID: databaseProjectID, Name: "Beta"},
	})
	deps := cliruntime.Deps{CommandPathPrefix: "volcano cloud", HTTPClient: server.Client(), APIBaseURL: server.URL}
	return executeDatabaseCommand(t, New(deps), append([]string{"ping"}, args...)...)
}

// The database API takes no platform token, so the test sends the CLI's
// reserved data-plane key, not whichever project key lists first, and never
// prints it.
func TestPingConnectsWithTheCLIDataPlaneKey(t *testing.T) {
	api := newPingAPI(t)

	out, err := runPingCommand(t, api.serve(), "app")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected to database 'app' in ")
	assert.NotContains(t, out, pingServiceKey)
	pings, bearers, _ := api.sent()
	assert.Equal(t, []string{"/databases/app/query/ping"}, pings)
	assert.Equal(t, []string{"Bearer " + pingServiceKey}, bearers)
}

func TestPingTestsABranchAsJSON(t *testing.T) {
	api := newPingAPI(t)

	out, err := runPingCommand(t, api.serve(), "app", "--branch", "preview", "--json")
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "app", result["database"])
	assert.Equal(t, "preview", result["branch"])
	assert.Contains(t, result, "latency_ms")
	pings, _, _ := api.sent()
	assert.Equal(t, []string{"/databases/app/branches/preview/query/ping"}, pings)
}

// The reserved key is created, least-privilege, on first use, as function
// invoke and storage commands do.
func TestPingCreatesTheCLIDataPlaneKeyOnFirstUse(t *testing.T) {
	api := newPingAPI(t)
	api.keys = []any{}

	out, err := runPingCommand(t, api.serve(), "app")
	require.NoError(t, err)
	assert.NotContains(t, out, "sk-created-key-value")
	api.mu.Lock()
	created := api.created
	api.mu.Unlock()
	require.Len(t, created, 1)
	assert.Equal(t, dataplane.CLIServiceKeyName, created[0]["name"])
	assert.NotContains(t, created[0]["permissions"], "*")
	_, bearers, _ := api.sent()
	assert.Equal(t, []string{"Bearer sk-created-key-value"}, bearers)
}

// A service key in VOLCANO_TOKEN already authenticates the database API, and
// the control plane would refuse it a key listing.
func TestPingSendsAServiceKeySessionTokenAsIs(t *testing.T) {
	api := newPingAPI(t)

	_, err := runPingCommandAs(t, api.serve(), "sk-session-key", "app")
	require.NoError(t, err)
	_, bearers, listedKeys := api.sent()
	assert.Equal(t, []string{"Bearer sk-session-key"}, bearers)
	assert.Zero(t, listedKeys)
}

func TestPingReportsAFailedTest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		message string
		args    []string
		want    []string
	}{
		{
			name: "missing database", status: http.StatusNotFound, message: "Database not found",
			args: []string{"app"}, want: []string{`database "app" not found`},
		},
		{
			name: "branch unavailable", status: http.StatusServiceUnavailable, message: "branch compute is starting",
			args: []string{"app", "--branch", "preview"},
			want: []string{`could not connect to branch "preview" of database "app"`, "branch compute is starting"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := newPingAPI(t)
			api.status, api.message = tc.status, tc.message

			out, err := runPingCommand(t, api.serve(), tc.args...)
			require.Error(t, err)
			for _, want := range tc.want {
				require.ErrorContains(t, err, want)
			}
			assert.NotContains(t, err.Error(), pingServiceKey)
			assert.NotContains(t, out, pingServiceKey)
		})
	}
}

// Local mode's session token is already its service key, and it has no
// branches.
func TestLocalPingSendsTheLocalServiceKey(t *testing.T) {
	api := newPingAPI(t)
	server := api.serve()
	setDatabaseCommandTestHome(t)
	serviceKeyDeps := cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		ConfigLoader: func() (*cliconfig.Config, error) {
			return &cliconfig.Config{
				UserToken:      "sk-local",
				CurrentProject: &cliconfig.ProjectConfig{ID: databaseProjectID, Name: "local"},
			}, nil
		},
	}
	cmd := NewLocalWithOptions(cliruntime.Deps{LocalMode: true}, LocalOptions{ServiceKeyDeps: &serviceKeyDeps})

	out, err := executeDatabaseCommand(t, cmd, "ping", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected to database 'app'")
	_, bearers, listedKeys := api.sent()
	assert.Equal(t, []string{"Bearer sk-local"}, bearers)
	assert.Zero(t, listedKeys)

	cmd = NewLocalWithOptions(cliruntime.Deps{LocalMode: true}, LocalOptions{ServiceKeyDeps: &serviceKeyDeps})
	_, err = executeDatabaseCommand(t, cmd, "ping", "app", "--branch", "preview")
	require.ErrorContains(t, err, "local development has no database branches")
	require.ErrorContains(t, err, "volcano cloud databases ping app --branch preview")
	pings, _, _ := api.sent()
	assert.Len(t, pings, 1)
}

// Local help offers no example local development refuses.
func TestLocalPingHelpLeavesBranchesOut(t *testing.T) {
	serviceKeyDeps := cliruntime.Deps{}
	out, err := executeDatabaseCommand(t,
		NewLocalWithOptions(cliruntime.Deps{LocalMode: true}, LocalOptions{ServiceKeyDeps: &serviceKeyDeps}),
		"ping", "--help")
	require.NoError(t, err)
	assert.NotContains(t, out, "--branch preview")
	assert.Contains(t, out, "Local development authenticates the test with its own service key.")
}
