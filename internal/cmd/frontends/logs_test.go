package frontends

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

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const otherFrontendDeploymentID = "77777777-7777-4777-8777-777777777777"

func TestFrontendsLogs(t *testing.T) {
	t.Run("runtime pages with next token", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var logBodies []map[string]any
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
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/logs/search":
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				logBodies = append(logBodies, body)
				if body["cursor"] == nil {
					writeFrontendCommandJSON(t, w, http.StatusOK, frontendLogCommandResponse("first runtime", true, "next token"))
					return
				}
				assert.Equal(t, "next token", body["cursor"])
				writeFrontendCommandJSON(t, w, http.StatusOK, frontendLogCommandResponse("second runtime", false, ""))
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "web", "--type", "runtime", "--limit", "2")
		require.NoError(t, err)
		require.Len(t, logBodies, 2)
		resource, ok := logBodies[0]["resource"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "frontend", resource["type"])
		assert.Equal(t, []any{frontendID}, resource["ids"])
		assert.InEpsilon(t, 2, logBodies[0]["limit"], 0)
		assert.NotContains(t, logBodies[0], "cursor")
		assert.Equal(t, "next token", logBodies[1]["cursor"])
		assert.Contains(t, out, "Fetching runtime logs for frontend web")
		assert.Contains(t, out, "first runtime")
		assert.Contains(t, out, "second runtime")
	})

	t.Run("runtime follow streams", func(t *testing.T) {
		api.ResetLastInstructionsForTest()
		t.Cleanup(api.ResetLastInstructionsForTest)
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var streamBody map[string]any
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
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/logs/stream":
				require.NoError(t, json.NewDecoder(r.Body).Decode(&streamBody))
				w.Header().Set("X-Volcano-CLI-Instruction", api.CLIInstructionSuggestionVersionUpgrade)
				w.Header().Set("X-Volcano-CLI-Latest-Version", "v1.5.0")
				writeFrontendLogStream(t, w, "runtime follow")
				// A healthy backend holds the connection open and tails new
				// events, so keep it open until the client cancels.
				<-r.Context().Done()
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/logs/search":
				t.Errorf("runtime --follow should use logs/stream")
				http.NotFound(w, r)
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		out, errCh := streamFrontendsCommand(ctx, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "web", "--type", "runtime", "--follow", "--limit", "2")

		require.Eventually(t, func() bool {
			text := out.String()
			return strings.Contains(text, "runtime follow") && strings.Contains(text, "A newer Volcano CLI version is available: v1.5.0")
		}, 2*time.Second, 10*time.Millisecond)
		cancel()
		require.NoError(t, <-errCh)

		resource, ok := streamBody["resource"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "frontend", resource["type"])
		assert.Equal(t, []any{frontendID}, resource["ids"])
		assert.NotContains(t, resource, "deployments")
		assert.InEpsilon(t, 2, streamBody["limit"], 0)
		assert.Contains(t, out.String(), "Following runtime logs for frontend web")
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

	t.Run("build follow streams deployment", func(t *testing.T) {
		setFrontendCommandTestHome(t)
		saveFrontendCommandTestConfig(t)
		var streamBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
				frontend := frontendCommandPayload(frontendID, "web")
				delete(frontend, "current_deployment_id")
				writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{frontend},
					"has_more": false,
					"page":     1,
					"limit":    100,
					"total":    1,
				})
			case r.Method == http.MethodGet && r.URL.Path == "/projects/"+frontendProjectID+"/frontends/"+frontendID+"/deployments":
				writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{frontendDeploymentCommandPayload(otherFrontendDeploymentID)},
					"has_more": false,
					"page":     1,
					"limit":    100,
					"total":    1,
				})
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/logs/stream":
				require.NoError(t, json.NewDecoder(r.Body).Decode(&streamBody))
				writeFrontendLogStream(t, w, "build follow")
			case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/logs/search":
				// After the stream ends and the deployment is terminal, the
				// follow loop runs a catch-up search that must suppress logs
				// already printed from the stream (id "stream-log").
				writeFrontendCommandJSON(t, w, http.StatusOK, catchUpLogResponse())
			default:
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "logs", "web", otherFrontendDeploymentID, "--type", "build", "--follow")
		require.NoError(t, err)
		resource, ok := streamBody["resource"].(map[string]any)
		require.True(t, ok)
		deployments, ok := resource["deployments"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "frontend", resource["type"])
		assert.Equal(t, []any{frontendID}, resource["ids"])
		assert.Equal(t, []any{otherFrontendDeploymentID}, deployments["ids"])
		assert.Contains(t, out, "Following build logs for frontend web deployment "+otherFrontendDeploymentID)
		assert.Contains(t, out, "build follow")
		// The catch-up search backfills logs not seen on the stream...
		assert.Contains(t, out, "catch up log")
		// ...without reprinting the streamed log.
		assert.Equal(t, 1, strings.Count(out, "build follow"))
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
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/logs/search":
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			resource, ok := body["resource"].(map[string]any)
			require.True(t, ok)
			deployments, ok := resource["deployments"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "frontend", resource["type"])
			assert.Equal(t, []any{frontendID}, resource["ids"])
			assert.Equal(t, []any{logDeploymentID}, deployments["ids"])
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

func writeFrontendLogStream(t *testing.T, w http.ResponseWriter, message string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write([]byte(": connected\n\n"))
	_, _ = w.Write([]byte("id: stream-cursor\n"))
	_, _ = w.Write([]byte("event: log\n"))
	_, _ = w.Write([]byte(`data: {"id":"stream-log","message":"` + message + `","timestamp":"2025-10-09T08:53:20Z","resource":{"type":"frontend","id":"` + frontendID + `"}}` + "\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
