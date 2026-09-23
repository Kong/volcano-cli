package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	serviceKeyID    = "33333333-3333-4333-8333-333333333333"
	serviceKeyValue = "sk-live-secret-that-is-only-safe-on-success"
	projectGammaID  = "44444444-4444-4444-8444-444444444444"
)

func TestProjectServiceKeysListUsesPageAndSelectedProject(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID, Name: "Beta"},
	})

	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/projects/"+projectBetaID+"/service-keys", r.URL.Path)
		gotQuery = r.URL.RawQuery
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{serviceKeyPayload()}, "has_more": true, "page": 2, "limit": 25, "total": 51,
		})
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"service-keys", "list", "--page", "2", "--limit", "25")
	require.NoError(t, err)
	assert.Equal(t, "page=2&limit=25", gotQuery)
	for _, want := range []string{"ID: " + serviceKeyID, "Name: admin", "Prefix: sk-prefix", "Permissions: functions.invoke, storage.read", "Key value: " + serviceKeyValue, "Created:", "Updated:", "Showing 1 of 51 service key(s)", "projects service-keys list --page 3 --limit 25"} {
		assert.Contains(t, out, want)
	}
}

func TestProjectServiceKeysListRejectsAWindowTheAPIWouldNot(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID, Name: "Beta"},
	})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request for an invalid window", r.URL.String())
	}))
	defer server.Close()

	for _, test := range []struct {
		args    []string
		message string
	}{
		{args: []string{"service-keys", "list", "--page", "0"}, message: "invalid --page 0: expected 1 or more"},
		{args: []string{"service-keys", "list", "--page", "-5"}, message: "invalid --page -5: expected 1 or more"},
		{args: []string{"service-keys", "list", "--limit", "0"}, message: "invalid --limit 0: expected 1 to 100"},
		{args: []string{"service-keys", "list", "--limit", "101"}, message: "invalid --limit 101: expected 1 to 100"},
	} {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			_, err := executeProjectCommand(t,
				NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), test.args...)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestProjectServiceKeysExplicitProjectOverridesEnvironment(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID, Name: "Beta"},
	})
	t.Setenv("VOLCANO_PROJECT_ID", projectAlphaID)

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{serviceKeyPayload()}, "has_more": true, "page": 1, "limit": 100, "total": 101,
		})
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"service-keys", "list", projectGammaID)
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectGammaID+"/service-keys", gotPath)
	assert.Contains(t, out, "projects service-keys list "+projectGammaID+" --page 2 --limit 100")
	assert.NotContains(t, out, "projects service-keys list --page")
}

func TestProjectServiceKeysCreatePreservesOmittedPermissions(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token", CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID}})
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeProjectCommandJSON(t, w, http.StatusCreated, serviceKeyPayload())
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"service-keys", "create", "admin")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"name": "admin"}, body)
	assert.Contains(t, out, "Key value: "+serviceKeyValue)
}

func TestProjectServiceKeysCreateSendsPermissions(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token", CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID}})
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeProjectCommandJSON(t, w, http.StatusCreated, serviceKeyPayload())
	}))
	defer server.Close()

	_, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"service-keys", "create", "scoped", "--permission", "functions.invoke", "--permission", "storage.read")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"name": "scoped", "permissions": []any{"functions.invoke", "storage.read"}}, body)
}

func TestProjectServiceKeysCreateMergesPermissionAliases(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token", CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID}})
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeProjectCommandJSON(t, w, http.StatusCreated, serviceKeyPayload())
	}))
	defer server.Close()

	_, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"service-keys", "create", "scoped", "--permission", "functions.invoke", "--permissions", "storage.read")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"name": "scoped", "permissions": []any{"functions.invoke", "storage.read"}}, body)
}

func TestProjectServiceKeysCreateRejectsEmptyPermission(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token", CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID}})
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		writeProjectCommandJSON(t, w, http.StatusCreated, serviceKeyPayload())
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"service-keys", "create", "scoped", "--permission", "functions.invoke", "--permissions", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service key permission cannot be empty")
	assert.False(t, called)
	assert.NotContains(t, out, serviceKeyValue)
}

func TestProjectServiceKeysGetUsesExplicitProject(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeProjectCommandJSON(t, w, http.StatusOK, serviceKeyPayload())
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"service-key", "get", serviceKeyID, projectAlphaID)
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectAlphaID+"/service-keys/"+serviceKeyID, gotPath)
	assert.Contains(t, out, "Key value: "+serviceKeyValue)
}

func TestProjectServiceKeysRejectsMissingCreateName(t *testing.T) {
	cmd := NewProjects(cliruntime.Deps{})
	_, err := executeProjectCommand(t, cmd, "service-keys", "create")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "accepts between 1 and 2 arg(s)")
}

func TestProjectServiceKeysNeverPrintsSecretOnError(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token", CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"` + serviceKeyID + `","name":"admin","key_value":"` + serviceKeyValue + `"}`))
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"service-keys", "create", "admin")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), serviceKeyValue)
	assert.NotContains(t, out, serviceKeyValue)
	assert.NotContains(t, err.Error()+"\n"+out, "sk-live-secret")
}

func serviceKeyPayload() map[string]any {
	return map[string]any{
		"id": serviceKeyID, "name": "admin", "key_prefix": "sk-prefix", "key_value": serviceKeyValue,
		"permissions": []string{"functions.invoke", "storage.read"}, "created_at": "2026-09-19T00:00:00Z", "updated_at": "2026-09-19T01:00:00Z",
	}
}
