package variables

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	variableProjectID = "22222222-2222-4222-8222-222222222222"

	secretSentinelAPIOldValue    = "SECRET_SENTINEL_API_OLD_VALUE"
	secretSentinelAPINewValue    = "SECRET_SENTINEL_API_NEW_VALUE"
	secretSentinelDebugValue     = "SECRET_SENTINEL_DEBUG_VALUE"
	secretSentinelUnchangedValue = "SECRET_SENTINEL_UNCHANGED_VALUE"
)

func TestVariableCommandsDeployListGetDelete(t *testing.T) {
	setVariableCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	envFile := strings.Join([]string{
		"API_KEY=" + secretSentinelAPINewValue,
		"DEBUG=" + secretSentinelDebugValue,
		"UNCHANGED=" + secretSentinelUnchangedValue,
		"",
	}, "\n")
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte(envFile), 0o644))
	saveVariableCommandTestConfig(t, &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   variableProjectID,
			Name: "Beta",
		},
	})

	values := map[string]string{
		"API_KEY":   secretSentinelAPIOldValue,
		"UNCHANGED": secretSentinelUnchangedValue,
	}
	var listQueries []string
	var createBodies []map[string]string
	var sawDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+variableProjectID+"/variables":
			listQueries = append(listQueries, r.URL.RawQuery)
			writeVariableCommandJSON(t, w, http.StatusOK, variableCommandListPayload(values))
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+variableProjectID+"/variables":
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			createBodies = append(createBodies, body)
			values[body["name"]] = body["value"]
			writeVariableCommandJSON(t, w, http.StatusCreated, variableCommandPayload(variableProjectID, body["name"], body["value"]))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+variableProjectID+"/variables/API_KEY":
			writeVariableCommandJSON(t, w, http.StatusOK, variableCommandPayload(variableProjectID, "API_KEY", values["API_KEY"]))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+variableProjectID+"/variables/API_KEY":
			sawDelete = true
			delete(values, "API_KEY")
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeVariableCommand(t, New(deps), "deploy")
	require.NoError(t, err)
	assert.ElementsMatch(t, []map[string]string{
		{"name": "API_KEY", "value": secretSentinelAPINewValue},
		{"name": "DEBUG", "value": secretSentinelDebugValue},
		{"name": "UNCHANGED", "value": secretSentinelUnchangedValue},
	}, createBodies)
	for _, want := range []string{"Reading volcano/volcano.env", "Found 3 variable(s)", "+ API_KEY", "+ DEBUG", "+ UNCHANGED", "3 variable(s) saved and propagation started"} {
		assert.Contains(t, out, want)
	}
	for _, secret := range []string{secretSentinelAPIOldValue, secretSentinelAPINewValue, secretSentinelDebugValue, secretSentinelUnchangedValue} {
		assert.NotContains(t, out, secret)
	}

	out, err = executeVariableCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Equal(t, []string{"page=1&limit=100"}, listQueries)
	for _, want := range []string{"API_KEY", "DEBUG", "UNCHANGED", "active", "Showing 3 of 3 variable(s) (page 1, limit 100)"} {
		assert.Contains(t, out, want)
	}
	for _, secret := range []string{secretSentinelAPINewValue, secretSentinelDebugValue, secretSentinelUnchangedValue} {
		assert.NotContains(t, out, secret)
	}

	out, err = executeVariableCommand(t, New(deps), "get", "API_KEY")
	require.NoError(t, err)
	assert.Contains(t, out, "Name: API_KEY")
	assert.NotContains(t, out, secretSentinelAPINewValue)
	assert.NotContains(t, out, "Value:")

	out, err = executeVariableCommand(t, New(deps), "delete", "API_KEY", "--yes")
	require.NoError(t, err)
	assert.True(t, sawDelete)
	assert.Contains(t, out, "Variable 'API_KEY' deleted and propagation started")
}

func TestVariablesListHonorsPaginationFlags(t *testing.T) {
	setVariableCommandTestHome(t)
	saveVariableCommandTestConfig(t, &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   variableProjectID,
			Name: "Beta",
		},
	})

	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/projects/"+variableProjectID+"/variables", r.URL.Path)
		queries = append(queries, r.URL.RawQuery)
		writeVariableCommandJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{variableCommandPayload(variableProjectID, "DEBUG", secretSentinelDebugValue)},
			"has_more": true,
			"page":     2,
			"limit":    1,
			"total":    3,
		})
	}))
	defer server.Close()

	out, err := executeVariableCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list", "--page", "2", "--limit", "1")
	require.NoError(t, err)
	assert.Equal(t, []string{"page=2&limit=1"}, queries)
	for _, want := range []string{
		"DEBUG",
		"Showing 1 of 3 variable(s) (page 2, limit 1)",
		"Next page: volcano variables list --page 3 --limit 1",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, secretSentinelDebugValue)
}

// --shared sets every variable in the file to the membership asked for. Without
// it nothing is sent, so the server keeps its default: a new variable is shared
// and an existing one keeps its setting.
func TestVariablesDeployShared(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		shared any
		saved  string
	}{
		{"omitted", []string{"deploy"}, nil, "+ API_KEY (saved)\n"},
		{"not shared", []string{"deploy", "--shared=false"}, false, "+ API_KEY (saved, not shared)\n"},
		{"shared", []string{"deploy", "--shared"}, true, "+ API_KEY (saved, shared)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setVariableCommandTestHome(t)
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll("volcano", 0o755))
			require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("API_KEY="+secretSentinelAPINewValue+"\nDEBUG="+secretSentinelDebugValue+"\n"), 0o600))
			saveVariableCommandTestConfig(t, &cliconfig.Config{
				UserToken:      "token",
				CurrentProject: &cliconfig.ProjectConfig{ID: variableProjectID, Name: "Beta"},
			})

			var bodies []map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "/projects/"+variableProjectID+"/variables", r.URL.Path)
				var body map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				bodies = append(bodies, body)
				payload := variableCommandPayload(variableProjectID, body["name"].(string), body["value"].(string))
				payload["shared"] = true
				if shared, sent := body["shared"]; sent {
					payload["shared"] = shared
				}
				writeVariableCommandJSON(t, w, http.StatusCreated, payload)
			}))
			defer server.Close()

			out, err := executeVariableCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), tc.args...)
			require.NoError(t, err)
			require.Len(t, bodies, 2)
			for _, body := range bodies {
				shared, sent := body["shared"]
				if tc.shared == nil {
					assert.False(t, sent, "nothing is sent without --shared")
				} else {
					assert.Equal(t, tc.shared, shared)
				}
			}
			assert.Contains(t, out, tc.saved)
			assert.NotContains(t, out, secretSentinelAPINewValue)
		})
	}
}

// A server where saving a variable as not shared is off refuses it with
// feature_unavailable, and the user sees its reason, not a claim that
// anything was saved.
func TestVariablesDeployShowsTheServersRefusalOfNotShared(t *testing.T) {
	setVariableCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("API_KEY="+secretSentinelAPINewValue+"\n"), 0o600))
	saveVariableCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: variableProjectID, Name: "Beta"},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeVariableCommandJSON(t, w, http.StatusBadRequest, map[string]any{
			"error": "shared: false is not available; omit shared or set it to true",
			"code":  "feature_unavailable",
		})
	}))
	defer server.Close()

	out, err := executeVariableCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--shared=false")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to create variable "API_KEY"`)
	assert.Contains(t, err.Error(), "shared: false is not available; omit shared or set it to true")
	assert.NotContains(t, out, "saved")
	assert.NotContains(t, out+err.Error(), secretSentinelAPINewValue)
}

// A server that predates shared accepts the field, ignores it, and saves the
// variable with its default. The CLI must not report the value it asked for.
func TestVariablesDeployRefusesToReportSharedTheServerIgnored(t *testing.T) {
	for _, tc := range []struct {
		name     string
		flag     string
		returned any
	}{
		{"no shared in the response", "--shared=false", nil},
		{"the default in the response", "--shared=false", true},
		{"not shared in the response to --shared", "--shared=true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setVariableCommandTestHome(t)
			t.Chdir(t.TempDir())
			require.NoError(t, os.MkdirAll("volcano", 0o755))
			require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("API_KEY="+secretSentinelAPINewValue+"\n"), 0o600))
			saveVariableCommandTestConfig(t, &cliconfig.Config{
				UserToken:      "token",
				CurrentProject: &cliconfig.ProjectConfig{ID: variableProjectID, Name: "Beta"},
			})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				payload := variableCommandPayload(variableProjectID, "API_KEY", secretSentinelAPINewValue)
				if tc.returned != nil {
					payload["shared"] = tc.returned
				}
				writeVariableCommandJSON(t, w, http.StatusCreated, payload)
			}))
			defer server.Close()

			out, err := executeVariableCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", tc.flag)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "API_KEY was saved, but the server did not apply "+tc.flag)
			assert.Contains(t, err.Error(), "Check it with volcano variables get API_KEY")
			assert.NotContains(t, out, "(saved")
			assert.NotContains(t, out, "propagation started")
			assert.NotContains(t, out+err.Error(), secretSentinelAPINewValue)
		})
	}
}

func TestVariableGetShowsWhetherItIsShared(t *testing.T) {
	setVariableCommandTestHome(t)
	saveVariableCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: variableProjectID, Name: "Beta"},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/projects/"+variableProjectID+"/variables/API_KEY", r.URL.Path)
		payload := variableCommandPayload(variableProjectID, "API_KEY", secretSentinelAPINewValue)
		payload["shared"] = false
		writeVariableCommandJSON(t, w, http.StatusOK, payload)
	}))
	defer server.Close()

	out, err := executeVariableCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "get", "API_KEY")
	require.NoError(t, err)
	assert.Contains(t, out, "Shared: no")
	assert.NotContains(t, out, secretSentinelAPINewValue)
}

func TestVariablesDeployEmptyEnvFile(t *testing.T) {
	setVariableCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("# comments only\n"), 0o644))

	out, err := executeVariableCommand(t, New(cliruntime.Deps{}), "deploy")
	require.NoError(t, err)
	assert.Contains(t, out, "No variables found in volcano/volcano.env")
}

func TestVariablesDeployLoadErrorIncludesPath(t *testing.T) {
	setVariableCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("API_KEY\n"), 0o644))

	out, err := executeVariableCommand(t, New(cliruntime.Deps{}), "deploy")
	require.ErrorContains(t, err, "failed to read volcano/volcano.env")
	assert.Empty(t, out)
}

func TestVariablesDeployRefusesNameValueArguments(t *testing.T) {
	setVariableCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("API_KEY=value\n"), 0o644))

	out, err := executeVariableCommand(t, New(cliruntime.Deps{}), "deploy", "FOO=bar")
	require.ErrorContains(t, err, `unknown command "FOO=bar"`)
	assert.NotContains(t, out, "API_KEY", "deploy must not run with arguments it would ignore")
}

func TestVariablesDeployRequiresProject(t *testing.T) {
	setVariableCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll("volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano.env"), []byte("API_KEY=value\n"), 0o644))
	saveVariableCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})

	for _, args := range [][]string{
		{"deploy"},
		{"list"},
		{"get", "API_KEY"},
		{"delete", "API_KEY", "--yes"},
	} {
		_, err := executeVariableCommand(t, New(cliruntime.Deps{}), args...)
		require.ErrorContains(t, err, "no project selected. Run 'volcano use <project-name>'", "%v", args)
	}
}

func executeVariableCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setVariableCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveVariableCommandTestConfig(t *testing.T, cfg *cliconfig.Config) {
	t.Helper()
	require.NoError(t, cfg.Save())
}

func writeVariableCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func variableCommandListPayload(values map[string]string) map[string]any {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)

	data := make([]any, 0, len(names))
	for _, name := range names {
		data = append(data, variableCommandPayload(variableProjectID, name, values[name]))
	}
	return map[string]any{
		"data":     data,
		"has_more": false,
		"page":     1,
		"limit":    100,
		"total":    len(data),
	}
}

func variableCommandPayload(projectID, name, value string) map[string]any {
	return map[string]any{
		"created_at": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		"id":         variableCommandID(name),
		"name":       name,
		"project_id": projectID,
		"status":     "active",
		"updated_at": time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339),
		"value":      value,
	}
}

func variableCommandID(name string) string {
	switch strings.TrimSpace(name) {
	case "API_KEY":
		return "33333333-3333-4333-8333-333333333333"
	case "DEBUG":
		return "44444444-4444-4444-8444-444444444444"
	case "UNCHANGED":
		return "55555555-5555-4555-8555-555555555555"
	default:
		return "66666666-6666-4666-8666-666666666666"
	}
}
