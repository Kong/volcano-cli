package schedulers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	testProjectID   = "22222222-2222-4222-8222-222222222222"
	testFunctionID  = "33333333-3333-4333-8333-333333333333"
	testSchedulerID = "44444444-4444-4444-8444-444444444444"
)

func TestSchedulersList(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionPayload(testFunctionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions/"+testFunctionID+"/schedulers":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					schedulerPayload(testSchedulerID, testFunctionID, testProjectID, "hello scheduler", true),
				},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list", "hello")
	require.NoError(t, err)
	assert.Contains(t, out, testSchedulerID)
	assert.Contains(t, out, "hello scheduler")
	assert.Contains(t, out, "enabled")
}

func TestSchedulersListEmpty(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionPayload(testFunctionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions/"+testFunctionID+"/schedulers":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    0,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list", "hello")
	require.NoError(t, err)
	assert.Contains(t, out, `No schedulers configured for function "hello"`)
}

func TestSchedulersCreate(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionPayload(testFunctionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+testProjectID+"/functions/"+testFunctionID+"/schedulers":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			writeJSON(t, w, http.StatusCreated, schedulerPayload(testSchedulerID, testFunctionID, testProjectID, "hello scheduler", true))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "hello",
		"--cron", "*/5 * * * *",
		"--name", "hello scheduler",
		"--payload", `{"k":"v"}`,
		"--regions", "aws-us-east-1",
	)
	require.NoError(t, err)
	assert.Contains(t, out, `Created scheduler for function "hello"`)
	assert.Equal(t, "hello scheduler", createBody["name"])
	schedule, ok := createBody["schedule"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "*/5 * * * *", schedule["cron_expression"])
	assert.Equal(t, map[string]any{"k": "v"}, createBody["payload"])
	assert.Equal(t, []any{"aws-us-east-1"}, createBody["regions"])
}

func TestSchedulersCreateLoadsPayloadFromFile(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	dir := t.TempDir()
	t.Chdir(dir)
	payloadPath := filepath.Join(dir, "payload.json")
	require.NoError(t, os.WriteFile(payloadPath, []byte(`{"from":"file"}`), 0o644))

	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionPayload(testFunctionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+testProjectID+"/functions/"+testFunctionID+"/schedulers":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			writeJSON(t, w, http.StatusCreated, schedulerPayload(testSchedulerID, testFunctionID, testProjectID, "hello scheduler", true))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "hello",
		"--cron", "*/5 * * * *",
		"--payload", "payload.json",
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"from": "file"}, createBody["payload"])
}

func TestSchedulersCreateDefaultsNameAndBubblesAPIError(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	var createBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionPayload(testFunctionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+testProjectID+"/functions/"+testFunctionID+"/schedulers":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			writeJSON(t, w, http.StatusBadRequest, map[string]any{
				"message": "cron expression is invalid",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "hello",
		"--cron", "*/5 * * * *",
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create scheduler")
	assert.Equal(t, "hello scheduler", createBody["name"], "default name should be '<function> scheduler' when --name is omitted")
}

func TestSchedulersCreateRejectsInvalidPayload(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "hello",
		"--cron", "*/5 * * * *",
		"--payload", "not-json",
	)
	require.ErrorContains(t, err, "payload must be a JSON object")
}

func TestSchedulersDisable(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	var updateBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionPayload(testFunctionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/"+testProjectID+"/functions/"+testFunctionID+"/schedulers/"+testSchedulerID:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updateBody))
			writeJSON(t, w, http.StatusOK, schedulerPayload(testSchedulerID, testFunctionID, testProjectID, "hello scheduler", false))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"disable", "hello", testSchedulerID,
	)
	require.NoError(t, err)
	assert.Equal(t, false, updateBody["enabled"])
	assert.Contains(t, out, "Disabled scheduler "+testSchedulerID)
	assert.Contains(t, out, "disabled")
}

func TestSchedulersDisableRejectsInvalidID(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	_, err := executeSchedulersCommand(t, New(cliruntime.Deps{}), "disable", "hello", "not-a-uuid")
	require.ErrorContains(t, err, "invalid scheduler id")
}

func TestSchedulersEnable(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	var updateBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionPayload(testFunctionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/"+testProjectID+"/functions/"+testFunctionID+"/schedulers/"+testSchedulerID:
			require.NoError(t, json.NewDecoder(r.Body).Decode(&updateBody))
			writeJSON(t, w, http.StatusOK, schedulerPayload(testSchedulerID, testFunctionID, testProjectID, "hello scheduler", true))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"enable", "hello", testSchedulerID,
	)
	require.NoError(t, err)
	assert.Equal(t, true, updateBody["enabled"])
	assert.Contains(t, out, "Enabled scheduler "+testSchedulerID)
	assert.Contains(t, out, "enabled")
}

func TestSchedulersEnableRejectsInvalidID(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	_, err := executeSchedulersCommand(t, New(cliruntime.Deps{}), "enable", "hello", "not-a-uuid")
	require.ErrorContains(t, err, "invalid scheduler id")
}

func TestSchedulersDeleteRejectsInvalidID(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	_, err := executeSchedulersCommand(t, New(cliruntime.Deps{}), "delete", "hello", "not-a-uuid")
	require.ErrorContains(t, err, "invalid scheduler id")
}

func TestSchedulersDelete(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+testProjectID+"/functions":
			writeJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionPayload(testFunctionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    25,
				"total":    1,
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+testProjectID+"/functions/"+testFunctionID+"/schedulers/"+testSchedulerID:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeSchedulersCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"delete", "hello", testSchedulerID, "--yes",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Deleted scheduler "+testSchedulerID)
}

func TestSchedulersDeletePromptsAndCancels(t *testing.T) {
	setTestHome(t)
	saveTestConfig(t)
	var sawDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetIn(strings.NewReader("no\n"))
	out, err := executeSchedulersCommand(t, cmd, "delete", "hello", testSchedulerID)
	require.NoError(t, err)
	assert.False(t, sawDelete)
	assert.Contains(t, out, "You are about to delete a resource permanently")
	assert.Contains(t, out, "Delete function scheduler '"+testSchedulerID+"'?")
	assert.Contains(t, out, "Delete cancelled.")
}

func executeSchedulersCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   testProjectID,
			Name: "Beta",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func functionPayload(id, name string) map[string]any {
	return map[string]any{
		"created_at":       "2026-05-20T00:00:00Z",
		"deployed_regions": []string{"aws-us-east-1"},
		"handler":          "handler",
		"id":               id,
		"invoke_url":       "https://" + id + ".functions.volcano.dev/",
		"is_public":        true,
		"name":             name,
		"project_id":       testProjectID,
		"runtime":          "nodejs24.x",
		"status":           "active",
		"updated_at":       "2026-05-20T00:00:00Z",
	}
}

func schedulerPayload(id, functionID, projectID, name string, enabled bool) map[string]any {
	return map[string]any{
		"created_at":      "2026-05-20T00:00:00Z",
		"cron_expression": "*/5 * * * *",
		"enabled":         enabled,
		"function_id":     functionID,
		"id":              id,
		"name":            name,
		"project_id":      projectID,
		"regions":         []string{"aws-us-east-1"},
		"schedule_kind":   "cron",
		"updated_at":      "2026-05-20T00:00:00Z",
	}
}
