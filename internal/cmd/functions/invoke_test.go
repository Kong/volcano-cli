package functions

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

func TestFunctionsInvokeByIDSkipsNameResolution(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)

	var invokeBody map[string]any
	var listHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/functions/"+functionID+"/invoke":
			require.NoError(t, json.NewDecoder(r.Body).Decode(&invokeBody))
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{"ok": true})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			listHits++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "invoke", "--id", functionID, "--payload", `{"k":"v"}`)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"payload": map[string]any{"k": "v"}}, invokeBody)
	assert.Equal(t, 0, listHits)
	assert.Contains(t, out, "{\n  \"ok\": true\n}\n")
}

func TestFunctionsInvokeByAliasTakesPrecedenceOverNameResolution(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/functions/"+otherFunctionID+"/invoke":
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{"aliased": true})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			t.Fatalf("alias invoke should not list functions")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	cfg.SetFunctionAlias(cliconfig.FunctionAliasScope(server.URL, functionProjectID), "hello", otherFunctionID)
	require.NoError(t, cfg.Save())

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "invoke", "hello", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"aliased":true}`, out)
}

func TestFunctionsInvokeByNameFallsBackToFunctionResolution(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)

	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionCommandPayload(functionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/functions/"+functionID+"/invoke":
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{"name": "hello"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "invoke", "volcano/functions/hello.js", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"hello"}`, out)
	assert.Equal(t, []string{
		"GET /projects/" + functionProjectID + "/functions?page=1&limit=100",
		"POST /functions/" + functionID + "/invoke",
	}, requests)
}

func TestFunctionsInvokeLocalUsesAnonKeyForInvokeOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "cloud-token")
	t.Setenv("VOLCANO_PROJECT_ID", "99999999-9999-4999-8999-999999999999")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	var sawListAuth string
	var sawInvokeAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			sawListAuth = r.Header.Get("Authorization")
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
				"data":     []any{functionCommandPayload(functionID, "hello")},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    1,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/functions/"+functionID+"/invoke":
			sawInvokeAuth = r.Header.Get("Authorization")
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		ConfigLoader: func() (*cliconfig.Config, error) {
			return &cliconfig.Config{
				APIBaseURL: server.URL,
				UserToken:  "local-token",
				AnonKey:    "local-anon-key",
				CurrentProject: &cliconfig.ProjectConfig{
					ID:   functionProjectID,
					Name: "local-dev",
				},
				IgnoreEnv: true,
			}, nil
		},
	}

	out, err := executeFunctionsCommand(t, NewLocal(deps), "invoke", "hello", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, out)
	assert.Equal(t, "Bearer local-token", sawListAuth)
	assert.Equal(t, "Bearer local-anon-key", sawInvokeAuth)
}

func TestFunctionsInvokeRejectsInvalidTargetsAndPayload(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "missing target", args: []string{"invoke"}, want: "specify a function name or --id"},
		{name: "name and id", args: []string{"invoke", "hello", "--id", functionID}, want: "specify either a function name or --id, not both"},
		{name: "bad payload", args: []string{"invoke", "hello", "--payload", "not-json"}, want: "payload must be a JSON object"},
		{name: "array payload", args: []string{"invoke", "hello", "--payload", "[]"}, want: "payload must be a JSON object"},
		{name: "bad id", args: []string{"invoke", "--id", "not-a-uuid"}, want: "invalid function ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := executeFunctionsCommand(t, New(cliruntime.Deps{}), tc.args...)
			require.Error(t, err)
			assert.Contains(t, out, "Error:")
			assert.ErrorContains(t, err, tc.want)
		})
	}
}

func TestFunctionsAliasSetListDelete(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})

	out, err := executeFunctionsCommand(t, cmd, "alias", "set", "hello", functionID)
	require.NoError(t, err)
	assert.Contains(t, out, "Alias: hello")
	assert.Contains(t, out, "Function ID: "+functionID)
	assert.Contains(t, out, `Set function alias "hello"`)

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	got, ok := cfg.FunctionAlias(cliconfig.FunctionAliasScope(server.URL, functionProjectID), "hello")
	require.True(t, ok)
	assert.Equal(t, functionID, got)

	out, err = executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "alias", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "Alias")
	assert.Contains(t, out, "hello")
	assert.Contains(t, out, functionID)

	out, err = executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "alias", "delete", "hello")
	require.NoError(t, err)
	assert.Contains(t, out, `Deleted function alias "hello"`)

	out, err = executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "alias", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No function aliases configured")
}
