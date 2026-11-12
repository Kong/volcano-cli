package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	rootcmd "github.com/Kong/volcano-cli/internal/cmd/root"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// testGrant is a delegated grant as a worker gets it. Every test here checks
// that it never shows up in the output.
const testGrant = cliconfig.DelegatedGrantPrefix + "grant-secret-value"

// grantRun is one command run the way a Builder worker runs the CLI.
type grantRun struct {
	code     int
	stdout   string
	stderr   string
	requests []string
	auth     []string
}

// runAsWorker runs the real command tree with the worker's environment: the
// grant in VOLCANO_TOKEN, the API in VOLCANO_API_URL, the project from the
// directory's link file, telemetry off and no prompts. No config loader is
// injected, so the CLI reads its configuration the way it does in the VM.
func runAsWorker(t *testing.T, handler http.HandlerFunc, args ...string) grantRun {
	t.Helper()
	resetInstructions(t)
	api.ResetLastRefusalForTest()
	t.Cleanup(api.ResetLastRefusalForTest)

	var mu sync.Mutex
	result := grantRun{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		result.requests = append(result.requests, r.Method+" "+r.URL.Path)
		result.auth = append(result.auth, r.Header.Get("Authorization"))
		mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(server.Close)

	home := t.TempDir()
	workspace := filepath.Join(home, "workspace", "app")
	require.NoError(t, os.MkdirAll(filepath.Join(workspace, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".volcano", "project.json"),
		[]byte(`{"project_id":"`+mainTestProjectAlpha+`"}`), 0o644))
	t.Setenv("HOME", home)
	t.Chdir(workspace)
	t.Setenv("VOLCANO_TOKEN", testGrant)
	t.Setenv("VOLCANO_API_URL", server.URL)
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_TELEMETRY_DISABLED", "1")
	t.Setenv("VOLCANO_NONINTERACTIVE", "1")

	deps := cliruntime.Deps{HTTPClient: server.Client()}
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	result.code = run(root, deps)
	result.stdout, result.stderr = stdout.String(), stderr.String()

	assert.NotContains(t, result.stdout, "grant-secret-value", "the grant was printed")
	assert.NotContains(t, result.stderr, "grant-secret-value", "the grant was printed")
	for _, request := range result.requests {
		assert.NotContains(t, request, "/cli/telemetry", "the grant reached telemetry")
		assert.NotContains(t, request, "/service-keys", "a grant must never cause a service key")
		assert.NotContains(t, request, "/user", "a grant must never reach an account route")
	}
	return result
}

// projectFunctionsPath is the path `cloud functions list` reads.
const projectFunctionsPath = "/projects/" + mainTestProjectAlpha + "/functions"

func TestGrantCallsProjectRoutesWithTheGrant(t *testing.T) {
	run := runAsWorker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == projectFunctionsPath {
			respondExitTestJSON(w, http.StatusOK, `{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`)
			return
		}
		respondExitTestJSON(w, http.StatusForbidden, `{"error":"delegated grants cannot be used on this endpoint","code":"grant_route_not_allowed"}`)
	}, "cloud", "functions", "list")

	assert.Equal(t, 0, run.code, run.stderr)
	assert.Equal(t, []string{"GET " + projectFunctionsPath}, run.requests)
	assert.Equal(t, []string{"Bearer " + testGrant}, run.auth)
	assert.NotContains(t, run.stderr, "VOLCANO_PROJECT_ID is deprecated", "the link file chose the project")
}

func TestGrantRefusalsExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   int
		hint   string
	}{
		{
			name:   "route not allowed",
			status: http.StatusForbidden,
			body:   `{"error":"delegated grants cannot be used on this endpoint","code":"grant_route_not_allowed"}`,
			code:   exitPermissionDenied,
			hint:   "Delegated grants cannot call GET " + projectFunctionsPath + ", so this command does not work with one.",
		},
		{
			name:   "permission missing",
			status: http.StatusForbidden,
			body:   `{"error":"the delegated grant does not carry the permission this endpoint needs","code":"grant_permission_missing"}`,
			code:   exitPermissionDenied,
			hint:   "The delegated grant lacks the permission GET " + projectFunctionsPath + " needs.",
		},
		{
			name:   "budget exhausted",
			status: http.StatusTooManyRequests,
			body:   `{"error":"the delegated grant has no mutations left; ask the app for a new grant","code":"grant_budget_exhausted"}`,
			code:   exitGrantBudgetExhausted,
			hint:   "HTTP 429: the delegated grant has no mutations left; ask the app for a new grant",
		},
		{
			// Any other 429, such as the per-grant rate limit, is not the
			// budget: retrying after a pause works, a new grant is not needed.
			name:   "rate limited",
			status: http.StatusTooManyRequests,
			body:   `{"error":"rate limit exceeded","code":"rate_limit_exceeded"}`,
			code:   exitFailure,
		},
		{
			name:   "expired or revoked",
			status: http.StatusUnauthorized,
			body:   `{"error":"invalid or expired token"}`,
			code:   exitNotAuthenticated,
			hint:   "The delegated grant has expired or was revoked. Get a new grant and run the command again.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runAsWorker(t, func(w http.ResponseWriter, _ *http.Request) {
				respondExitTestJSON(w, tc.status, tc.body)
			}, "cloud", "functions", "list")

			assert.Equal(t, tc.code, run.code, run.stderr)
			if tc.hint != "" {
				assert.Contains(t, run.stderr, tc.hint)
			}
			assert.NotContains(t, run.stderr, "only works on the project it was", "not a project mismatch")
		})
	}
}

// A grant was an unknown prefix, so an account token, before the CLI knew it.
// Account commands now refuse it before any request.
func TestGrantIsRefusedOnAccountCommandsBeforeAnyRequest(t *testing.T) {
	for _, args := range [][]string{
		{"projects", "list"},
		{"cloud", "functions", "list", "--project", "some-project-name"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			run := runAsWorker(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("an account call reached the API: %s %s", r.Method, r.URL.Path)
				respondExitTestJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
			}, args...)

			assert.Equal(t, exitPermissionDenied, run.code, run.stderr)
			assert.Contains(t, run.stderr, "delegated grant (pg-)")
			assert.Empty(t, run.requests)
		})
	}
}

// The wrong-project 403 gets the mismatch hint, worded for a grant.
func TestGrantMismatchHintNamesTheGrant(t *testing.T) {
	run := runAsWorker(t, func(w http.ResponseWriter, _ *http.Request) {
		respondExitTestJSON(w, http.StatusForbidden, `{"error":"project access token is not valid for this project"}`)
	}, "cloud", "functions", "list")

	assert.Equal(t, exitPermissionDenied, run.code, run.stderr)
	assert.Contains(t, run.stderr, "A delegated grant only works on the project it was granted for")
}

// testFunctionID is the function the invoke tests call.
const testFunctionID = "99999999-9999-4999-8999-999999999999"

// Invoke sends the grant itself to the data-plane invoke route. It never
// lists, reads or creates the CLI's service key (runAsWorker fails any
// /service-keys request). Hosting accepts a grant there from C6 on.
func TestGrantInvokesWithTheGrantAndNoServiceKey(t *testing.T) {
	run := runAsWorker(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/functions/"+testFunctionID+"/invoke" {
			respondExitTestJSON(w, http.StatusOK, `{"ok":true}`)
			return
		}
		respondExitTestJSON(w, http.StatusForbidden, `{"error":"delegated grants cannot be used on this endpoint","code":"grant_route_not_allowed"}`)
	}, "cloud", "functions", "invoke", "--id", testFunctionID, "--json")

	assert.Equal(t, 0, run.code, run.stderr)
	assert.Equal(t, []string{"POST /functions/" + testFunctionID + "/invoke"}, run.requests)
	assert.Equal(t, []string{"Bearer " + testGrant}, run.auth)
	assert.JSONEq(t, `{"ok":true}`, run.stdout)
}

// Storage object commands need the CLI's service key, so a grant refuses them
// before any request.
func TestGrantRefusesStorageObjectCommandsBeforeAnyRequest(t *testing.T) {
	run := runAsWorker(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a storage object command reached the API: %s %s", r.Method, r.URL.Path)
		respondExitTestJSON(w, http.StatusForbidden, `{"error":"forbidden"}`)
	}, "cloud", "storage", "object", "list", "uploads")

	assert.Equal(t, exitPermissionDenied, run.code, run.stderr)
	assert.Contains(t, run.stderr, "storage object commands do not work with a delegated grant (pg-)")
	assert.Empty(t, run.requests)
}
