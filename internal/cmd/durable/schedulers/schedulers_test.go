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
	projectID   = "22222222-2222-4222-8222-222222222222"
	functionID  = "55555555-5555-4555-8555-555555555555"
	schedulerID = "77777777-7777-4777-8777-777777777777"
)

func TestSchedulersListPopulatedAndEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		body map[string]any
		want []string
	}{
		{
			name: "populated",
			body: map[string]any{"data": []any{schedulerPayload("order-pipeline scheduler", true)}},
			want: []string{schedulerID, "order-pipeline scheduler", "enabled", "0 * * * *", "aws-us-east-1"},
		},
		{
			name: "none configured",
			body: map[string]any{"data": []any{}},
			want: []string{`No schedulers configured for durable function "order-pipeline"`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setSchedulersTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/projects/"+projectID+"/durable-functions/order-pipeline/schedulers", r.URL.Path)
				writeJSON(t, w, http.StatusOK, tc.body)
			}))
			defer server.Close()

			out, err := executeCommand(t, newSchedulersCommand(server), "list", "order-pipeline")
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
		})
	}
}

// The durable collection is addressed by name or id, and the two collections
// never accept each other's, so a create must go to the durable path rather
// than resolving the name against /functions first.
func TestSchedulersCreatePostsToTheDurableCollection(t *testing.T) {
	setSchedulersTestHome(t)
	var body map[string]any
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch {
		case r.Method == http.MethodPost &&
			r.URL.Path == "/projects/"+projectID+"/durable-functions/order-pipeline/schedulers":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			writeJSON(t, w, http.StatusCreated, schedulerPayload("order-pipeline scheduler", true))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeCommand(t, newSchedulersCommand(server),
		"create", "order-pipeline", "--cron", "0 * * * *")
	require.NoError(t, err)
	assert.Equal(t, "order-pipeline scheduler", body["name"], "default name should be '<function> scheduler'")
	assert.Equal(t, map[string]any{"cron_expression": "0 * * * *"}, body["schedule"])
	assert.NotContains(t, body, "payload", "no --input should send no payload at all")
	assert.Contains(t, out, `Created scheduler for durable function "order-pipeline"`)
	assert.NotContains(t, paths, "/projects/"+projectID+"/functions")
}

func TestSchedulersCreateSendsNameRegionsAndInput(t *testing.T) {
	setSchedulersTestHome(t)
	dir := t.TempDir()
	t.Chdir(dir)
	inputPath := filepath.Join(dir, "sweep.json")
	require.NoError(t, os.WriteFile(inputPath, []byte(`{"scope":"nightly"}`), 0o600))

	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeJSON(t, w, http.StatusCreated, schedulerPayload("nightly-sweep", true))
	}))
	defer server.Close()

	_, err := executeCommand(t, newSchedulersCommand(server),
		"create", "order-pipeline",
		"--name", "nightly-sweep",
		"--cron", "0 2 * * *",
		"--input", "sweep.json",
		"--regions", "aws-us-east-1",
	)
	require.NoError(t, err)
	assert.Equal(t, "nightly-sweep", body["name"])
	assert.Equal(t, map[string]any{"scope": "nightly"}, body["payload"])
	assert.Equal(t, []any{"aws-us-east-1"}, body["regions"])
}

func TestSchedulersCreateRefusesMalformedInput(t *testing.T) {
	setSchedulersTestHome(t)

	_, err := executeCommand(t, newSchedulersCommand(nil),
		"create", "order-pipeline", "--cron", "0 * * * *", "--input", "not-json")
	require.ErrorContains(t, err, "input must be a JSON object")
}

func TestSchedulersCreateRequiresACron(t *testing.T) {
	setSchedulersTestHome(t)

	_, err := executeCommand(t, newSchedulersCommand(nil), "create", "order-pipeline")
	require.ErrorContains(t, err, `required flag(s) "cron" not set`)
}

func TestSchedulersEnableAndDisable(t *testing.T) {
	for _, tc := range []struct {
		name        string
		args        []string
		wantEnabled bool
		want        string
	}{
		{
			name:        "enable",
			args:        []string{"enable", "order-pipeline", schedulerID},
			wantEnabled: true,
			want:        "Enabled scheduler " + schedulerID,
		},
		{
			name:        "disable",
			args:        []string{"disable", "order-pipeline", schedulerID},
			wantEnabled: false,
			want:        "Disabled scheduler " + schedulerID,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setSchedulersTestHome(t)
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPatch, r.Method)
				assert.Equal(t,
					"/projects/"+projectID+"/durable-functions/order-pipeline/schedulers/"+schedulerID,
					r.URL.Path)
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				writeJSON(t, w, http.StatusOK, schedulerPayload("order-pipeline scheduler", tc.wantEnabled))
			}))
			defer server.Close()

			out, err := executeCommand(t, newSchedulersCommand(server), tc.args...)
			require.NoError(t, err)
			assert.Equal(t, tc.wantEnabled, body["enabled"])
			assert.Len(t, body, 1, "only the enabled flag should be sent")
			assert.Contains(t, out, tc.want)
		})
	}
}

func TestSchedulersDelete(t *testing.T) {
	setSchedulersTestHome(t)
	var deleted string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeCommand(t, newSchedulersCommand(server),
		"delete", "order-pipeline", schedulerID, "--yes")
	require.NoError(t, err)
	assert.Equal(t,
		"/projects/"+projectID+"/durable-functions/order-pipeline/schedulers/"+schedulerID, deleted)
	assert.Contains(t, out, "Deleted scheduler "+schedulerID)
}

func TestSchedulersDeletePromptsAndCancels(t *testing.T) {
	setSchedulersTestHome(t)
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

	cmd := newSchedulersCommand(server)
	cmd.SetIn(strings.NewReader("no\n"))
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"delete", "order-pipeline", schedulerID})
	require.NoError(t, cmd.Execute())

	assert.False(t, sawDelete)
	assert.Contains(t, out.String(), "Delete durable function scheduler '"+schedulerID+"'?")
	assert.Contains(t, out.String(), "Delete cancelled.")
}

// A scheduler id is checked before anything is sent, so a typo cannot be
// mistaken for a scheduler that is missing.
func TestSchedulersRefuseAMalformedSchedulerID(t *testing.T) {
	for _, args := range [][]string{
		{"enable", "order-pipeline", "not-a-uuid"},
		{"disable", "order-pipeline", "not-a-uuid"},
		{"delete", "order-pipeline", "not-a-uuid", "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			setSchedulersTestHome(t)

			_, err := executeCommand(t, newSchedulersCommand(nil), args...)
			require.ErrorContains(t, err, `invalid scheduler id "not-a-uuid"`)
		})
	}
}

// A scheduler route answers 404 for an unknown durable function as readily as
// for an unknown scheduler, so the message names both subjects and carries the
// API's own answer about which was missing.
func TestSchedulersNameBothSubjectsOnNotFound(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
		args    []string
	}{
		{
			name:    "unknown function",
			message: "durable function not found",
			args:    []string{"disable", "order-pipeline", schedulerID},
		},
		{
			name:    "unknown scheduler",
			message: "scheduler not found",
			args:    []string{"delete", "order-pipeline", schedulerID, "--yes"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setSchedulersTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(t, w, http.StatusNotFound, map[string]any{"error": tc.message})
			}))
			defer server.Close()

			_, err := executeCommand(t, newSchedulersCommand(server), tc.args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), `scheduler "`+schedulerID+`"`)
			assert.Contains(t, err.Error(), `durable function "order-pipeline"`)
			assert.Contains(t, err.Error(), tc.message)
		})
	}
}

// A durable function's name is not accepted by the standard collection, so a
// list that lands there is a bug the message has to survive.
func TestSchedulersListReportsAMissingFunction(t *testing.T) {
	setSchedulersTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]any{"error": "durable function not found"})
	}))
	defer server.Close()

	_, err := executeCommand(t, newSchedulersCommand(server), "list", "hello")
	require.ErrorContains(t, err, `durable function "hello" not found`)
}

func newSchedulersCommand(server *httptest.Server) *cobra.Command {
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

func setSchedulersTestHome(t *testing.T) {
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

func schedulerPayload(name string, enabled bool) map[string]any {
	return map[string]any{
		"id":              schedulerID,
		"project_id":      projectID,
		"function_id":     functionID,
		"function_kind":   "durable",
		"name":            name,
		"enabled":         enabled,
		"schedule_kind":   "cron",
		"cron_expression": "0 * * * *",
		"regions":         []string{"aws-us-east-1"},
		"created_at":      "2026-05-20T00:00:00Z",
		"updated_at":      "2026-05-20T00:00:00Z",
	}
}
