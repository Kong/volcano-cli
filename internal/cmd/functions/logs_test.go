package functions

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

	t.Run("runtime follow streams", func(t *testing.T) {
		setFunctionCommandTestHome(t)
		saveFunctionCommandTestConfig(t)
		var streamBody map[string]any
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
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/logs/stream":
				require.NoError(t, json.NewDecoder(r.Body).Decode(&streamBody))
				writeFunctionLogStream(t, w, "runtime follow")
				// A healthy backend holds the connection open and tails new
				// events, so keep it open until the client cancels.
				<-r.Context().Done()
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/logs/search":
				t.Errorf("runtime --follow should use logs/stream")
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		out, errCh := streamFunctionsCommand(ctx, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "hello", "--type", "runtime", "--follow", "--limit", "2")

		require.Eventually(t, func() bool {
			return strings.Contains(out.String(), "runtime follow")
		}, 2*time.Second, 10*time.Millisecond)
		cancel()
		require.NoError(t, <-errCh)

		resource, ok := streamBody["resource"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "function", resource["type"])
		assert.Equal(t, []any{functionID}, resource["ids"])
		assert.NotContains(t, resource, "deployments")
		assert.InEpsilon(t, 2, streamBody["limit"], 0)
		assert.Contains(t, out.String(), "Following runtime logs for function hello")
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

	t.Run("build follow streams deployment", func(t *testing.T) {
		setFunctionCommandTestHome(t)
		saveFunctionCommandTestConfig(t)
		var streamBody map[string]any
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
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions/"+functionID+"/deployments":
				writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{deploymentCommandPayload(otherDeploymentID)},
					"has_more": false,
					"page":     1,
					"limit":    100,
					"total":    1,
				})
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/logs/stream":
				require.NoError(t, json.NewDecoder(r.Body).Decode(&streamBody))
				writeFunctionLogStream(t, w, "build follow")
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/logs/search":
				// After the stream ends and the deployment is terminal, the
				// follow loop runs a catch-up search that must suppress logs
				// already printed from the stream (id "stream-log").
				writeFunctionCommandJSON(t, w, http.StatusOK, catchUpLogResponse())
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "hello", otherDeploymentID, "--type", "build", "--follow")
		require.NoError(t, err)
		resource, ok := streamBody["resource"].(map[string]any)
		require.True(t, ok)
		deployments, ok := resource["deployments"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "function", resource["type"])
		assert.Equal(t, []any{functionID}, resource["ids"])
		assert.Equal(t, []any{otherDeploymentID}, deployments["ids"])
		assert.Contains(t, out, "Following build logs for function hello deployment "+otherDeploymentID)
		assert.Contains(t, out, "build follow")
		// The catch-up search backfills logs not seen on the stream...
		assert.Contains(t, out, "catch up log")
		// ...without reprinting the streamed log.
		assert.Equal(t, 1, strings.Count(out, "build follow"))
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
				"timestamp": "2025-10-09T08:53:20Z",
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

func catchUpLogResponse() map[string]any {
	return map[string]any{
		"data": []any{
			map[string]any{
				"id":        "stream-log",
				"message":   "build follow",
				"region":    "aws-us-east-1",
				"timestamp": "2025-10-09T08:53:20Z",
			},
			map[string]any{
				"id":        "catch-up-log",
				"message":   "catch up log",
				"region":    "aws-us-east-1",
				"timestamp": "2025-10-09T08:53:20.001Z",
			},
		},
		"has_more": false,
		"limit":    100,
		"page":     1,
		"total":    2,
	}
}

func writeFunctionLogStream(t *testing.T, w http.ResponseWriter, message string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write([]byte(": connected\n\n"))
	_, _ = w.Write([]byte("id: stream-cursor\n"))
	_, _ = w.Write([]byte("event: log\n"))
	_, _ = w.Write([]byte(`data: {"id":"stream-log","message":"` + message + `","timestamp":"2025-10-09T08:53:20Z","resource":{"type":"function","id":"` + functionID + `"}}` + "\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
