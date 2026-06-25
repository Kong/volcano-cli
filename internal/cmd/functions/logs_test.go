package functions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	functionDeploymentID = "55555555-5555-4555-8555-555555555555"
	otherDeploymentID    = "66666666-6666-4666-8666-666666666666"
)

func TestFunctionsLogs(t *testing.T) {
	t.Run("runtime pages with next token", func(t *testing.T) {
		setFunctionCommandTestHome(t)
		saveFunctionCommandTestConfig(t)
		var logBodies []map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
				writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{functionCommandPayload(functionID, "hello")},
					"has_more": false,
					"page":     1,
					"limit":    100,
					"total":    1,
				})
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/logs/search":
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				logBodies = append(logBodies, body)
				if body["cursor"] == nil {
					writeFunctionCommandJSON(t, w, http.StatusOK, logCommandResponse("first runtime", true, "next token"))
					return
				}
				assert.Equal(t, "next token", body["cursor"])
				writeFunctionCommandJSON(t, w, http.StatusOK, logCommandResponse("second runtime", false, ""))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "hello", "--type", "runtime", "--limit", "2")
		require.NoError(t, err)
		require.Len(t, logBodies, 2)
		resource, ok := logBodies[0]["resource"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "function", resource["type"])
		assert.Equal(t, []any{functionID}, resource["ids"])
		assert.InEpsilon(t, 2, logBodies[0]["limit"], 0)
		assert.NotContains(t, logBodies[0], "cursor")
		assert.Equal(t, "next token", logBodies[1]["cursor"])
		assert.Contains(t, out, "Fetching runtime logs for function hello")
		assert.Contains(t, out, "first runtime")
		assert.Contains(t, out, "second runtime")
	})

	t.Run("build defaults latest deployment", func(t *testing.T) {
		setFunctionCommandTestHome(t)
		saveFunctionCommandTestConfig(t)
		server := functionLogsBuildServer(t, functionDeploymentID, true)
		defer server.Close()

		out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "hello", "--type", "build")
		require.NoError(t, err)
		assert.Contains(t, out, "Fetching build logs for function hello deployment "+functionDeploymentID)
		assert.Contains(t, out, "build log")
	})

	t.Run("build explicit deployment", func(t *testing.T) {
		setFunctionCommandTestHome(t)
		saveFunctionCommandTestConfig(t)
		server := functionLogsBuildServer(t, otherDeploymentID, false)
		defer server.Close()

		out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "hello", otherDeploymentID, "--type", "build")
		require.NoError(t, err)
		assert.Contains(t, out, "Fetching build logs for function hello deployment "+otherDeploymentID)
		assert.Contains(t, out, "build log")
	})

	t.Run("validates type", func(t *testing.T) {
		_, err := executeFunctionsCommand(t, New(cliruntime.Deps{}), "logs", "hello", "--type", "deploy")
		require.ErrorContains(t, err, "--type must be one of: build, runtime")

		_, err = executeFunctionsCommand(t, New(cliruntime.Deps{}), "logs", "hello", functionDeploymentID, "--type", "runtime")
		require.ErrorContains(t, err, "deployment-id is only supported with --type build")
	})
}

func functionLogsBuildServer(t *testing.T, logDeploymentID string, includeCurrentDeployment bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			function := functionCommandPayload(functionID, "hello")
			if includeCurrentDeployment {
				function["current_deployment_id"] = logDeploymentID
			}
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{function},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions/"+functionID+"/deployments":
			if includeCurrentDeployment {
				t.Errorf("default build logs should use current_deployment_id instead of listing deployments")
				http.NotFound(w, r)
				return
			}
			if r.URL.Query().Get("page") == "1" && logDeploymentID == otherDeploymentID {
				writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{deploymentCommandPayload(functionDeploymentID)},
					"has_more": true,
					"page":     1,
					"limit":    100,
					"total":    2,
				})
				return
			}
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{deploymentCommandPayload(logDeploymentID)},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/logs/search":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			resource, ok := body["resource"].(map[string]any)
			require.True(t, ok)
			deployments, ok := resource["deployments"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "function", resource["type"])
			assert.Equal(t, []any{functionID}, resource["ids"])
			assert.Equal(t, []any{logDeploymentID}, deployments["ids"])
			writeFunctionCommandJSON(t, w, http.StatusOK, logCommandResponse("build log", false, ""))
		default:
			http.NotFound(w, r)
		}
	}))
}

func deploymentCommandPayload(id string) map[string]any {
	return map[string]any{
		"created_at":  "2026-05-20T00:00:00Z",
		"function_id": functionID,
		"id":          id,
		"operation":   "deploy",
		"project_id":  functionProjectID,
		"status":      "active",
		"updated_at":  "2026-05-20T00:00:00Z",
	}
}

func logCommandResponse(message string, hasMore bool, next string) map[string]any {
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
