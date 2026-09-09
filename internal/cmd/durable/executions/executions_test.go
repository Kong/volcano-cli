package executions

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	projectID   = "22222222-2222-4222-8222-222222222222"
	executionID = "66666666-6666-4666-8666-666666666666"
)

func TestExecutionsListPopulatedAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  map[string]any
		query string
		args  []string
		want  []string
	}{
		{
			name: "populated",
			body: map[string]any{
				"data": []any{
					executionPayload(executionID, "order-4417", "running"),
				},
				"has_more": true,
				"page":     1,
				"limit":    100,
				"total":    2,
			},
			query: "page=1&limit=100",
			args:  []string{"list", "order-pipeline"},
			want: []string{
				executionID,
				"order-4417",
				"running",
				"Showing 1 of 2 execution(s) (page 1, limit 100)",
				"Next page: volcano cloud durable executions list order-pipeline --page 2 --limit 100",
			},
		},
		{
			name: "filtered",
			body: map[string]any{
				"data": []any{
					executionPayload(executionID, "order-4417", "succeeded"),
				},
				"has_more": true,
				"page":     1,
				"limit":    100,
				"total":    2,
			},
			query: "page=1&limit=100&status=succeeded",
			args:  []string{"list", "order-pipeline", "--status", "succeeded"},
			want: []string{
				"Next page: volcano cloud durable executions list order-pipeline --status succeeded --page 2 --limit 100",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setExecutionsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/projects/"+projectID+"/durable-functions/order-pipeline/executions", r.URL.Path)
				assert.ElementsMatch(t, splitQuery(tc.query), splitQuery(r.URL.RawQuery))
				writeJSON(t, w, http.StatusOK, tc.body)
			}))
			defer server.Close()

			out, err := executeCommand(t, newExecutionsCommand(server), tc.args...)
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
		})
	}
}

// Reading an execution is what refreshes its status, and a result that is past
// retention has to read as gone rather than as an execution that produced
// nothing.
func TestExecutionsGetRendersOutcome(t *testing.T) {
	for _, tc := range []struct {
		name        string
		payload     map[string]any
		want        []string
		wantMissing []string
	}{
		{
			name: "succeeded with a result",
			payload: withFields(executionPayload(executionID, "order-4417", "succeeded"), map[string]any{
				"completed_at": "2026-05-20T00:05:00Z",
				"result":       map[string]any{"shipped": true},
			}),
			want: []string{"Status: succeeded", "Duration: 5m0s", `"shipped": true`},
		},
		{
			name: "succeeded with null result",
			payload: withFields(executionPayload(executionID, "order-4417", "succeeded"), map[string]any{
				"completed_at": "2026-05-20T00:05:00Z",
				"result":       nil,
			}),
			want: []string{"Result: null"},
		},
		{
			name: "result no longer retained",
			payload: withFields(executionPayload(executionID, "order-4417", "succeeded"), map[string]any{
				"completed_at":   "2026-05-20T00:05:00Z",
				"result_expired": true,
			}),
			want: []string{"Result: no longer retained"},
		},
		{
			name: "failed",
			payload: withFields(executionPayload(executionID, "order-4417", "failed"), map[string]any{
				"completed_at": "2026-05-20T00:05:00Z",
				"error":        map[string]any{"type": "Error", "message": "charge declined"},
			}),
			want:        []string{"Error Type: Error", "Error: charge declined"},
			wantMissing: []string{"Result:"},
		},
		{
			name:        "still running",
			payload:     executionPayload(executionID, "order-4417", "running"),
			want:        []string{"Status: running"},
			wantMissing: []string{"Result:", "Completed:"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setExecutionsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t,
					"/projects/"+projectID+"/durable-functions/order-pipeline/executions/"+executionID,
					r.URL.Path)
				writeJSON(t, w, http.StatusOK, tc.payload)
			}))
			defer server.Close()

			out, err := executeCommand(t, newExecutionsCommand(server), "get", "order-pipeline", executionID)
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
			for _, missing := range tc.wantMissing {
				assert.NotContains(t, out, missing)
			}
		})
	}
}

// An execution is addressed by id. Its name is only an idempotency key and is
// not unique over time, so a name has to be refused here rather than sent to a
// route that would read it as an id.
func TestExecutionsRefuseANameWhereAnIDIsRequired(t *testing.T) {
	for _, args := range [][]string{
		{"get", "order-pipeline", "order-4417"},
		{"stop", "order-pipeline", "order-4417", "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			setExecutionsTestHome(t)
			_, err := executeCommand(t, newExecutionsCommand(nil), args...)
			require.ErrorContains(t, err, `invalid execution ID "order-4417"`)
		})
	}
}

// An execution route answers 404 for an unknown function as readily as for an
// unknown execution. Those are different mistakes, so the message has to name
// both the execution asked for and the function it was asked of, and carry the
// API's own answer about which one was missing.
func TestExecutionsNameBothSubjectsOnNotFound(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		args    []string
		want    string
	}{
		{
			name:    "unknown function",
			message: "durable function not found",
			args:    []string{"get", "order-pipeline", executionID},
			want:    "durable function not found",
		},
		{
			name:    "unknown execution",
			message: "durable execution not found",
			args:    []string{"stop", "order-pipeline", executionID, "--yes"},
			want:    "durable execution not found",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setExecutionsTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusNotFound, map[string]any{"error": tc.message})
			}))
			defer server.Close()

			_, err := executeCommand(t, newExecutionsCommand(server), tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), `durable execution "`+executionID+`"`)
			assert.Contains(t, err.Error(), `durable function "order-pipeline"`)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestExecutionsStopConfirms(t *testing.T) {
	setExecutionsTestHome(t)
	stopped := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t,
			"/projects/"+projectID+"/durable-functions/order-pipeline/executions/"+executionID+"/stop",
			r.URL.Path)
		stopped = true
		writeJSON(t, w, http.StatusOK, executionPayload(executionID, "order-4417", "stopped"))
	}))
	defer server.Close()

	out, err := executeCommand(t, newExecutionsCommand(server), "stop", "order-pipeline", executionID)
	require.NoError(t, err)
	assert.True(t, stopped)
	assert.Contains(t, out, "Stop execution "+executionID+"?")
	assert.Contains(t, out, "Status: stopped")
	assert.Contains(t, out, "Stop requested for execution "+executionID)
}

func TestExecutionsStopDeclinedLeavesTheExecutionAlone(t *testing.T) {
	setExecutionsTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("stop must not be called when the prompt is declined")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cmd := newExecutionsCommand(server)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString("n\n"))
	cmd.SetArgs([]string{"stop", "order-pipeline", executionID})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Cancelled.")
}

func newExecutionsCommand(server *httptest.Server) *cobra.Command {
	deps := cliruntime.Deps{CommandPathPrefix: "volcano cloud"}
	if server != nil {
		deps.HTTPClient = server.Client()
		deps.APIBaseURL = server.URL
	}
	return New(deps)
}

func executeCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString("y\n"))
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setExecutionsTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	cfg := &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectID, Name: "Beta"},
	}
	require.NoError(t, cfg.Save())
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func executionPayload(id, name, status string) map[string]any {
	return map[string]any{
		"id":                  id,
		"durable_function_id": "55555555-5555-4555-8555-555555555555",
		"project_id":          projectID,
		"name":                name,
		"status":              status,
		"region":              "aws-us-east-1",
		"created_at":          "2026-05-20T00:00:00Z",
		"updated_at":          "2026-05-20T00:00:00Z",
	}
}

func withFields(payload, fields map[string]any) map[string]any {
	maps.Copy(payload, fields)
	return payload
}

func splitQuery(query string) []string {
	if query == "" {
		return nil
	}
	return strings.Split(query, "&")
}
