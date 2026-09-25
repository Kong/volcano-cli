package project

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestProjectUsageDefaultsToSelectedProjectAndOmitsSeries(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID, Name: "Beta"},
	})
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
			"project_id": projectBetaID,
			"month":      "2026-09",
			"metrics": []any{map[string]any{
				"metric": "Bandwidth Ingress (Bytes)", "total": 1024, "all_time": 4096,
				"hourly": []any{map[string]any{"timestamp": "2026-09-19T00:00:00Z", "value": 999}},
				"daily":  []any{map[string]any{"timestamp": "2026-09-19T00:00:00Z", "value": 888}},
			}},
		})
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "usage")
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectBetaID+"/usage", gotPath)
	assert.Contains(t, out, "Month: 2026-09")
	assert.Contains(t, out, "Bandwidth Ingress (Bytes)")
	assert.Contains(t, out, "1024")
	assert.Contains(t, out, "4096")
	assert.NotContains(t, out, "999")
	assert.NotContains(t, out, "888")
	assert.NotContains(t, out, "dollars")
	assert.NotContains(t, out, "quota")
}

func TestProjectUsageExplicitProjectOverridesEnvironment(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID},
	})
	t.Setenv("VOLCANO_PROJECT_ID", projectAlphaID)
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{"project_id": projectGammaID, "month": "2026-09", "metrics": []any{}})
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "usage", projectGammaID)
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectGammaID+"/usage", gotPath)
	assert.Contains(t, out, "No usage metrics recorded")
}
