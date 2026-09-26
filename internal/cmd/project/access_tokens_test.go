package project

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const projectAccessTokenID = "77777777-7777-4777-8777-777777777777"

func TestProjectAccessTokenKeys(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken: "token", CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID, Name: "Beta"},
	})

	base := "/projects/" + projectBetaID + "/access-tokens"
	token := map[string]any{
		"id": projectAccessTokenID, "project_id": projectBetaID, "name": "ci-deploy",
		"scope": "full", "status": "active", "token_prefix": "pt-Wq9l2m4X",
		"token_source": "cli", "all_time_requests": 42,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			created := maps.Clone(token)
			created["token"] = "pt-secret"
			writeProjectCommandJSON(t, w, http.StatusCreated, created)
		case r.Method == http.MethodGet && r.URL.Path == base:
			assert.Equal(t, "page=1&limit=1", r.URL.RawQuery)
			writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{token}, "page": 1, "limit": 1, "total": 2, "has_more": true,
			})
		case r.Method == http.MethodGet && r.URL.Path == base+"/"+projectAccessTokenID:
			writeProjectCommandJSON(t, w, http.StatusOK, token)
		case r.Method == http.MethodGet && r.URL.Path == base+"/usage":
			writeProjectCommandJSON(t, w, http.StatusOK, []any{})
		case r.Method == http.MethodDelete && r.URL.Path == base+"/"+projectAccessTokenID:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
	help, err := executeProjectCommand(t, NewProjects(deps), "keys", "--help")
	require.NoError(t, err)
	assert.Contains(t, help, "access-tokens")
	help, err = executeProjectCommand(t, NewProjects(deps), "keys", "access-tokens", "create", "--help")
	require.NoError(t, err)
	assert.Contains(t, help, "volcano projects keys access-tokens create ci-deploy")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"create", "ci-deploy"}, "pt-secret"},
		{[]string{"list", "--limit", "1"}, "Next page: volcano projects keys access-tokens list --page 2 --limit 1"},
		{[]string{"get", projectAccessTokenID}, "Name: ci-deploy"},
		{[]string{"usage"}, "No access tokens created"},
		{[]string{"revoke", projectAccessTokenID, "--yes"}, "Access token 'ci-deploy' revoked"},
	} {
		out, err := executeProjectCommand(t, NewProjects(deps), append([]string{"keys", "access-tokens"}, tc.args...)...)
		require.NoError(t, err, "%v", tc.args)
		assert.Contains(t, out, tc.want)
		if tc.args[0] != "create" {
			assert.NotContains(t, out, "pt-secret")
		}
	}
	assert.Equal(t, []string{
		"POST " + base, "GET " + base, "GET " + base + "/" + projectAccessTokenID,
		"GET " + base + "/usage", "GET " + base + "/" + projectAccessTokenID,
		"DELETE " + base + "/" + projectAccessTokenID,
	}, requests)
}
