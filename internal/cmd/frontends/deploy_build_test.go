package frontends

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	sourceFrontendID       = "77777777-7777-4777-8777-777777777777"
	servingDeploymentID    = "88888888-8888-4888-8888-888888888888"
	failedDeploymentID     = "99999999-9999-4999-8999-999999999999"
	queuedTargetDeployment = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
)

// buildReuseServer serves a project with the frontends web (the source) and
// web-eu (the target). Web's newest deployment failed; the one before it is
// what web serves.
type buildReuseServer struct {
	t           *testing.T
	refusal     map[string]any
	refusalCode int
	queued      bool
	deployedID  string
}

func (s *buildReuseServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := s.t
	frontends := "/projects/" + frontendProjectID + "/frontends"
	switch {
	case r.Method == http.MethodGet && r.URL.Path == frontends:
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				frontendCommandPayload(sourceFrontendID, "web"),
				frontendCommandPayload(frontendID, "web-eu"),
			},
			"has_more": false, "page": 1, "limit": 100, "total": 2,
		})
	case r.Method == http.MethodGet && r.URL.Path == frontends+"/"+sourceFrontendID+"/deployments":
		writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				frontendDeploymentPayload(failedDeploymentID, sourceFrontendID, "failed"),
				frontendDeploymentPayload(servingDeploymentID, sourceFrontendID, "active"),
			},
			"has_more": false, "page": 1, "limit": 100, "total": 2,
		})
	case r.Method == http.MethodPost && r.URL.Path == frontends+"/"+frontendID+"/deployments":
		var body struct {
			SourceDeploymentID string `json:"source_deployment_id"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		s.deployedID = body.SourceDeploymentID
		if s.refusal != nil {
			writeFrontendCommandJSON(t, w, s.refusalCode, s.refusal)
			return
		}
		payload := frontendCommandPayload(frontendID, "web-eu")
		if s.queued {
			payload["pending_deployment_id"] = queuedTargetDeployment
		}
		writeFrontendCommandJSON(t, w, http.StatusOK, payload)
	default:
		http.NotFound(w, r)
	}
}

func frontendDeploymentPayload(id, frontendID, status string) map[string]any {
	return map[string]any{
		"id":          id,
		"frontend_id": frontendID,
		"project_id":  frontendProjectID,
		"operation":   "redeploy",
		"status":      status,
		"created_at":  "2026-05-20T00:00:00Z",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}

func runBuildReuse(t *testing.T, server *buildReuseServer, local bool, args ...string) (string, error) {
	t.Helper()
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	server.t = t
	httpServer := httptest.NewServer(server)
	t.Cleanup(httpServer.Close)

	deps := cliruntime.Deps{HTTPClient: httpServer.Client(), APIBaseURL: httpServer.URL}
	if !local {
		deps.CommandPathPrefix = "volcano cloud"
		return executeFrontendsCommand(t, New(deps), args...)
	}
	deps.LocalMode = true
	deps.CommandPathPrefix = "volcano"
	deps.ConfigLoader = func() (*cliconfig.Config, error) {
		return &cliconfig.Config{
			APIBaseURL:     httpServer.URL,
			UserToken:      "token",
			CurrentProject: &cliconfig.ProjectConfig{ID: frontendProjectID, Name: "local"},
		}, nil
	}
	return executeFrontendsCommand(t, NewLocal(deps), args...)
}

func TestFrontendsDeployFromDeploysTheBuildTheSourceServes(t *testing.T) {
	server := &buildReuseServer{}
	out, err := runBuildReuse(t, server, false, "deploy", "web-eu", "--from", "web")

	require.NoError(t, err, out)
	assert.Equal(t, servingDeploymentID, server.deployedID, "a failed newest deployment is not what web serves")
	assert.Contains(t, out, "Frontend 'web-eu' deployment started with the build of 'web'")
	assert.Contains(t, out, "Source deployment: "+servingDeploymentID)
	assert.Contains(t, out, "Deployment: "+frontendDeploymentID)
}

func TestFrontendsDeployFromNamedDeployment(t *testing.T) {
	server := &buildReuseServer{queued: true}
	out, err := runBuildReuse(t, server, false, "deploy", "web-eu", "--from", "web@"+failedDeploymentID)

	require.NoError(t, err, out)
	assert.Equal(t, failedDeploymentID, server.deployedID, "Volcano decides whether a named deployment can be reused")
	assert.Contains(t, out, "Pending Deployment: "+queuedTargetDeployment)
}

func TestFrontendsDeployFromRefusesBeforeDeploying(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"no target", []string{"deploy", "--from", "web"}, "--from needs exactly one frontend to deploy to, as in 'volcano cloud frontends deploy web-eu --from web'"},
		{"target without --from", []string{"deploy", "web-eu"}, `unexpected argument "web-eu"`},
		{"source flags", []string{"deploy", "web-eu", "--from", "web", "--path", ".", "--variable-scope", "all"}, "--from deploys an existing build and cannot be combined with --path, --variable-scope"},
		{"empty deployment", []string{"deploy", "web-eu", "--from", "web@"}, `--from "web@" must be <frontend> or <frontend>@<deployment-id>`},
		{"malformed deployment", []string{"deploy", "web-eu", "--from", "web@v2"}, `--from "web@v2": "v2" is not a deployment ID`},
		{"unknown target", []string{"deploy", "web-us", "--from", "web"}, `frontend "web-us" not found`},
		{"unknown source", []string{"deploy", "web-eu", "--from", "docs"}, `frontend "docs" not found`},
		{"deployment of another frontend", []string{"deploy", "web-eu", "--from", "web@" + frontendDeploymentID}, `frontend "web" has no deployment ` + frontendDeploymentID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := &buildReuseServer{}
			out, err := runBuildReuse(t, server, false, tc.args...)

			require.Error(t, err, out)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, server.deployedID)
		})
	}
}

func TestFrontendsDeployFromNamesDifferingVariables(t *testing.T) {
	server := &buildReuseServer{refusalCode: http.StatusConflict, refusal: map[string]any{
		"error": "build variables differ from the source build: NEXT_PUBLIC_API_URL",
		"code":  "build_variables_differ",
	}}
	out, err := runBuildReuse(t, server, false, "deploy", "web-eu", "--from", "web")

	require.Error(t, err, out)
	assert.Contains(t, err.Error(), "build variables differ from the source build: NEXT_PUBLIC_API_URL")
	assert.Contains(t, err.Error(), "Redeploy 'web' if those variables changed since its build, or give 'web-eu' the same variable selection")
}

func TestFrontendsDeployFromIsCloudOnlyInLocalMode(t *testing.T) {
	server := &buildReuseServer{}
	out, err := runBuildReuse(t, server, true, "deploy", "web-eu", "--from", "web")

	require.Error(t, err, out)
	assert.Contains(t, err.Error(), "--from is a cloud option")
	assert.Contains(t, err.Error(), "run 'volcano frontends deploy --name web-eu' in the frontend's directory")
	assert.Contains(t, err.Error(), "'volcano cloud frontends deploy web-eu --from web' to deploy the build in the cloud project")
	assert.NotContains(t, out, "Deploying to local development", "the source-deploy note does not apply")
	assert.Empty(t, server.deployedID)
}
