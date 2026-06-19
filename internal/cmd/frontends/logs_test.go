package frontends

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const otherFrontendDeploymentID = "77777777-7777-4777-8777-777777777777"

func TestFrontendsLogs(t *testing.T) {
	t.Run("runtime pages with next token", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var logQueries []string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
				writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{frontendCommandPayload(frontendID, "web")},
					"has_more": false,
					"page":     1,
					"limit":    100,
					"total":    1,
				})
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/logs":
				logQueries = append(logQueries, r.URL.RawQuery)
				if r.URL.Query().Get("cursor") == "" {
					writeFrontendCommandJSON(t, w, http.StatusOK, frontendLogCommandResponse("first runtime", true, "next token"))
					return
				}
				assert.Equal(t, "next token", r.URL.Query().Get("cursor"))
				writeFrontendCommandJSON(t, w, http.StatusOK, frontendLogCommandResponse("second runtime", false, ""))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "web", "--type", "runtime", "--limit", "2")
		require.NoError(t, err)
		assert.Equal(t, []string{"limit=2", "limit=2&cursor=next+token"}, logQueries)
		assert.Contains(t, out, "Fetching runtime logs for frontend web")
		assert.Contains(t, out, "first runtime")
		assert.Contains(t, out, "second runtime")
	})

	t.Run("build defaults current deployment", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := frontendLogsBuildServer(t, frontendDeploymentID, true)
		defer server.Close()

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "web", "--type", "build")
		require.NoError(t, err)
		assert.Contains(t, out, "Fetching build logs for frontend web deployment "+frontendDeploymentID)
		assert.Contains(t, out, "build log")
	})

	t.Run("build explicit deployment", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		server := frontendLogsBuildServer(t, otherFrontendDeploymentID, false)
		defer server.Close()

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "web", otherFrontendDeploymentID, "--type", "build")
		require.NoError(t, err)
		assert.Contains(t, out, "Fetching build logs for frontend web deployment "+otherFrontendDeploymentID)
		assert.Contains(t, out, "build log")
	})

	t.Run("validates type", func(t *testing.T) {
		_, err := executeFrontendsCommand(t, New(cliruntime.Deps{}), "logs", "web", "--type", "deploy")
		require.ErrorContains(t, err, "--type must be one of: build, runtime")

		_, err = executeFrontendsCommand(t, New(cliruntime.Deps{}), "logs", "web", frontendDeploymentID, "--type", "runtime")
		require.ErrorContains(t, err, "deployment-id is only supported with --type build")
	})
}

func frontendLogsBuildServer(t *testing.T, logDeploymentID string, includeCurrentDeployment bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			frontend := frontendCommandPayload(frontendID, "web")
			if !includeCurrentDeployment {
				delete(frontend, "current_deployment_id")
			}
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{frontend},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/deployments":
			if includeCurrentDeployment {
				t.Errorf("default build logs should use current_deployment_id instead of listing deployments")
				http.NotFound(w, r)
				return
			}
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{frontendDeploymentCommandPayload(logDeploymentID)},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/deployments/"+logDeploymentID+"/logs":
			writeFrontendCommandJSON(t, w, http.StatusOK, frontendLogCommandResponse("build log", false, ""))
		default:
			http.NotFound(w, r)
		}
	}))
}

func frontendDeploymentCommandPayload(id string) map[string]any {
	return map[string]any{
		"created_at":  "2026-05-20T00:00:00Z",
		"frontend_id": frontendID,
		"id":          id,
		"operation":   "deploy",
		"project_id":  frontendProjectID,
		"status":      "active",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}

func frontendLogCommandResponse(message string, hasMore bool, next string) map[string]any {
	response := map[string]any{
		"data": []any{
			map[string]any{
				"message":   message,
				"region":    "aws-us-east-1",
				"timestamp": int64(1760000000000),
			},
		},
		"has_more": hasMore,
		"limit":    100,
		"page":     1,
		"total":    1,
	}
	if next != "" {
		response["next_cursor"] = next
	}
	return response
}
