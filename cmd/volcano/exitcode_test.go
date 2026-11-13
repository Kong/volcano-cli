package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Kong/volcano-cli/internal/api"
	rootcmd "github.com/Kong/volcano-cli/internal/cmd/root"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	exitTestApprovalID = "77777777-7777-4777-8777-777777777777"
	exitTestFrontendID = "88888888-8888-4888-8888-888888888888"
	needsAPersonError  = "approvals are decided by a person. " +
		"Run `volcano login` with VOLCANO_TOKEN unset, then decide again"
)

// runAgainst runs the real command tree with token and mainTestProjectAlpha
// selected, against handler, and returns the exit code and stderr.
func runAgainst(t *testing.T, token string, handler http.HandlerFunc, args ...string) (int, string) {
	t.Helper()
	resetInstructions(t)
	api.ResetLastRefusalForTest()
	t.Cleanup(api.ResetLastRefusalForTest)
	t.Setenv("HOME", t.TempDir())
	t.Chdir(t.TempDir())
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		ConfigLoader: func() (*cliconfig.Config, error) {
			return &cliconfig.Config{
				APIBaseURL:     server.URL,
				UserToken:      token,
				IgnoreEnv:      true,
				CurrentProject: &cliconfig.ProjectConfig{ID: mainTestProjectAlpha},
			}, nil
		},
	}
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(args)
	return run(root, deps), stderr.String()
}

func respondExitTestJSON(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// The binding read and the project read both answer 404 for a deleted
// project. The service names the project, and the exit code still reports the
// API's 404.
func TestRunGitStatusOnADeletedProjectExitsNotFound(t *testing.T) {
	code, stderr := runAgainst(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		respondExitTestJSON(w, http.StatusNotFound, `{"error":"project not found"}`)
	}, "git", "status")

	assert.Equal(t, exitNotFound, code, stderr)
	assert.Contains(t, stderr, "Error: the selected project does not exist: "+mainTestProjectAlpha+"\n")
}

// A credential the CLI knows is a project access token is refused before any
// request, and the refusal is a permission failure.
func TestRunApprovalDecisionByAProjectTokenExitsPermissionDenied(t *testing.T) {
	code, stderr := runAgainst(t, cliconfig.ProjectTokenPrefix+"scoped", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a project access token's decision must not reach the API: %s %s", r.Method, r.URL.Path)
		respondExitTestJSON(w, http.StatusNotFound, `{"error":"not found"}`)
	}, "cloud", "durable", "approvals", "approve", exitTestApprovalID)

	assert.Equal(t, exitPermissionDenied, code, stderr)
	assert.Contains(t, stderr, "Error: "+needsAPersonError+"\n")
}

// Turning LavaFlow on with a project access token is refused before any
// request, with the exit code of a permission the API refuses.
func TestRunLavaFlowEnableByAProjectTokenExitsPermissionDenied(t *testing.T) {
	code, stderr := runAgainst(t, cliconfig.ProjectTokenPrefix+"scoped", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a project access token must not turn LavaFlow on: %s %s", r.Method, r.URL.Path)
		respondExitTestJSON(w, http.StatusNotFound, `{"error":"not found"}`)
	}, "cloud", "lavaflow", "enable")

	assert.Equal(t, exitPermissionDenied, code, stderr)
	assert.Contains(t, stderr, "Error: changing LavaFlow's settings needs a person's session")
}

// The API's 403 for a credential the CLI did not recognize is rewritten into
// the same answer. The exit code reports the refusal, and no hint about the
// token's project is added.
func TestRunApprovalDecisionRefusedByTheAPIExitsPermissionDenied(t *testing.T) {
	code, stderr := runAgainst(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			respondExitTestJSON(w, http.StatusOK, `{"id":"`+exitTestApprovalID+`","status":"pending","name":"ship"}`)
			return
		}
		respondExitTestJSON(w, http.StatusForbidden, `{"error":"project access tokens cannot decide durable approvals; `+
			`a person decides in the dashboard or with a platform token"}`)
	}, "cloud", "durable", "approvals", "deny", exitTestApprovalID, "--yes")

	assert.Equal(t, exitPermissionDenied, code, stderr)
	assert.Contains(t, stderr, "Error: "+needsAPersonError+"\n")
	assert.NotContains(t, stderr, "only works on the project it was created in")
}

// Services that name the missing resource in their own words keep the API's
// 404, or the resolver's not-found, for the exit code.
func TestRunNamedNotFoundErrorsExitNotFound(t *testing.T) {
	notFound := func(w http.ResponseWriter, _ *http.Request) {
		respondExitTestJSON(w, http.StatusNotFound, `{"error":"not found"}`)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{
			name: "approval",
			args: []string{"cloud", "durable", "approvals", "deny", exitTestApprovalID, "--yes"},
			want: "Error: no approval " + exitTestApprovalID + " in this project\n",
		},
		{
			name: "durable function",
			args: []string{"cloud", "durable", "get", "hello"},
			want: "Error: durable function \"hello\" not found\n",
		},
		{
			name: "frontend by ID",
			args: []string{"cloud", "frontends", "get", exitTestFrontendID},
			want: "Error: frontend \"" + exitTestFrontendID + "\" not found\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := runAgainst(t, "test-token", notFound, tc.args...)

			assert.Equal(t, exitNotFound, code, stderr)
			assert.Contains(t, stderr, tc.want)
		})
	}
}

// A route the frontend doesn't have is a not-found, though the API answered
// the frontend and its routes.
func TestRunMissingFrontendRouteExitsNotFound(t *testing.T) {
	routesAt := "/projects/" + mainTestProjectAlpha + "/frontends/" + exitTestFrontendID + "/function-routes"
	mountsAt := "/projects/" + mainTestProjectAlpha + "/frontends/" + exitTestFrontendID + "/frontend-routes"
	code, stderr := runAgainst(t, "test-token", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/" + mainTestProjectAlpha + "/frontends":
			respondExitTestJSON(w, http.StatusOK, `{"data":[{"id":"`+exitTestFrontendID+`","project_id":"`+mainTestProjectAlpha+
				`","name":"web","framework":"nextjs","status":"ready","created_at":"2026-05-20T00:00:00Z","updated_at":"2026-05-20T00:00:00Z"}],`+
				`"has_more":false,"page":1,"limit":100,"total":1}`)
		case routesAt, mountsAt:
			respondExitTestJSON(w, http.StatusOK, `{"data":[]}`)
		default:
			respondExitTestJSON(w, http.StatusTeapot, `{"error":"unexpected `+r.Method+" "+r.URL.Path+`"}`)
		}
	}, "cloud", "frontends", "routes", "delete", "web", "/missing", "--yes")

	assert.Equal(t, exitNotFound, code, stderr)
	assert.Contains(t, stderr, `route "/missing" not found for frontend "web"`)
}

// A name no resolver page holds is a not-found too, though the API never
// answered 404.
func TestRunFunctionNameMissExitsNotFound(t *testing.T) {
	code, stderr := runAgainst(t, "test-token", func(w http.ResponseWriter, _ *http.Request) {
		respondExitTestJSON(w, http.StatusOK, `{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`)
	}, "cloud", "functions", "get", "missing")

	assert.Equal(t, exitNotFound, code, stderr)
	assert.Contains(t, stderr, "Error: function \"missing\" not found\n")
}
