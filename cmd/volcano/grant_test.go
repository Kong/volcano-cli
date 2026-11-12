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

// Hosting's refusals keep their exit codes and messages. Only a spent budget
// gets its own code, because a new grant is the fix.
func TestGrantRefusalsExitCodes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		code   int
		want   string
	}{
		{"route not allowed", http.StatusForbidden,
			`{"error":"delegated grants cannot be used on this endpoint","code":"grant_route_not_allowed"}`,
			exitPermissionDenied, "delegated grants cannot be used on this endpoint"},
		{"permission missing", http.StatusForbidden,
			`{"error":"the delegated grant does not carry the permission this endpoint needs","code":"grant_permission_missing"}`,
			exitPermissionDenied, "the delegated grant does not carry the permission this endpoint needs"},
		{"budget exhausted", http.StatusTooManyRequests,
			`{"error":"the delegated grant has no mutations left; ask the app for a new grant","code":"grant_budget_exhausted"}`,
			exitGrantBudgetExhausted, "the delegated grant has no mutations left; ask the app for a new grant"},
		// Any other 429, such as the per-grant rate limit, is not the budget.
		{"rate limited", http.StatusTooManyRequests,
			`{"error":"rate limit exceeded","code":"rate_limit_exceeded"}`, exitFailure, "rate limit exceeded"},
		{"expired or revoked", http.StatusUnauthorized,
			`{"error":"invalid or expired token"}`, exitNotAuthenticated, "invalid or expired token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := runAsWorker(t, func(w http.ResponseWriter, _ *http.Request) {
				respondExitTestJSON(w, tc.status, tc.body)
			}, "cloud", "functions", "list")

			assert.Equal(t, tc.code, run.code, run.stderr)
			assert.Contains(t, run.stderr, tc.want)
		})
	}
}

// A grant was an unknown prefix, so an account token, before the CLI knew it.
// Account commands now refuse it before any request, as they refuse a pt-.
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
			assert.Contains(t, run.stderr, "this command needs an account token")
			assert.Empty(t, run.requests)
		})
	}
}
