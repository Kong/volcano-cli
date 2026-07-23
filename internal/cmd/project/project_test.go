package project

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	projectAlphaID = "11111111-1111-4111-8111-111111111111"
	projectBetaID  = "22222222-2222-4222-8222-222222222222"
)

func TestProjectsOutputAndCurrentProject(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   projectBetaID,
			Name: "Beta",
		},
	})

	var queries []string
	freePlan := "FREE"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		queries = append(queries, r.URL.RawQuery)
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{projectCommandPayload(projectAlphaID, "Alpha", "active", &freePlan)},
			"has_more": false,
			"page":     1,
			"limit":    100,
			"total":    1,
		})
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}))
	require.NoError(t, err)
	assert.Equal(t, []string{"page=1&limit=100"}, queries)
	for _, want := range []string{"ID", "Name", "Status", "Plan", "Alpha", "FREE", "Showing 1 of 1 project(s) (page 1, limit 100)", "Current project: Beta (" + projectBetaID + ")"} {
		assert.Contains(t, out, want)
	}
}

func TestProjectsFetchesRequestedPage(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{projectCommandPayload(projectBetaID, "Beta", "active", nil)},
			"has_more": true,
			"page":     2,
			"limit":    25,
			"total":    51,
		})
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "--page", "2", "--limit", "25")
	require.NoError(t, err)
	assert.Equal(t, []string{"page=2&limit=25"}, queries)
	assert.Contains(t, out, "Beta")
	assert.Contains(t, out, "Showing 1 of 51 project(s) (page 2, limit 25)")
	assert.Contains(t, out, "Next page: volcano projects --page 3 --limit 25")
}

func TestProjectsListSubcommandFetchesRequestedPage(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
	var queries []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{projectCommandPayload(projectBetaID, "Beta", "active", nil)},
			"has_more": false,
			"page":     3,
			"limit":    10,
			"total":    21,
		})
	}))
	defer server.Close()

	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list", "--page", "3", "--limit", "10")
	require.NoError(t, err)
	assert.Equal(t, []string{"page=3&limit=10"}, queries)
	assert.Contains(t, out, "Beta")
	assert.Contains(t, out, "Showing 1 of 21 project(s) (page 3, limit 10)")
}

func TestUseByNameAndProjectCreateGetDelete(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
	var listRequests int
	var createRequests int
	var getRequests int
	var deleteRequests int
	var createPayload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			listRequests++
			writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{
					projectCommandPayload(projectAlphaID, "Alpha", "active", nil),
					projectCommandPayload(projectBetaID, "Beta", "active", nil),
				},
				"has_more": false,
				"page":     1,
				"limit":    100,
				"total":    2,
			})
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+projectAlphaID:
			getRequests++
			proPlan := "PRO"
			writeProjectCommandJSON(t, w, http.StatusOK, projectCommandPayload(projectAlphaID, "Alpha", "active", &proPlan))
		case r.Method == http.MethodPost && r.URL.Path == "/projects":
			createRequests++
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createPayload))
			writeProjectCommandJSON(t, w, http.StatusCreated, projectCommandPayload(projectAlphaID, "Alpha", "provisioning", nil))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+projectAlphaID:
			deleteRequests++
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeProjectCommand(t, NewUse(deps), "Beta")
	require.NoError(t, err)
	assert.Contains(t, out, "Now using project: Beta")
	cfg := loadProjectCommandTestConfig(t)
	require.NotNil(t, cfg.CurrentProject)
	assert.Equal(t, projectBetaID, cfg.CurrentProject.ID)
	assert.Equal(t, 1, listRequests)

	out, err = executeProjectCommand(t, NewProjects(deps), "use", "Beta")
	require.NoError(t, err)
	assert.Contains(t, out, "Now using project: Beta")
	assert.Equal(t, 2, listRequests)

	out, err = executeProjectCommand(t, NewProjects(deps), "create", " Alpha ")
	require.NoError(t, err)
	assert.Contains(t, out, "Project created: Alpha ("+projectAlphaID+")")
	assert.Equal(t, map[string]any{"name": "Alpha"}, createPayload)
	assert.Equal(t, 1, createRequests)

	out, err = executeProjectCommand(t, NewProjects(deps), "get", projectAlphaID)
	require.NoError(t, err)
	assert.Contains(t, out, "ID:     "+projectAlphaID)
	assert.Contains(t, out, "Plan:   PRO")
	assert.Equal(t, 1, getRequests)

	out, err = executeProjectCommand(t, NewProjects(deps), "delete", projectAlphaID, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Project deletion started: "+projectAlphaID)
	assert.Equal(t, 1, deleteRequests)

	out, err = executeProjectCommand(t, NewProjects(deps), "delete", projectAlphaID, "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Project deletion started: "+projectAlphaID)
	assert.Equal(t, 2, deleteRequests)
}

func TestProjectDeletePromptsAndCancels(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
	var sawDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			sawDelete = true
			w.WriteHeader(http.StatusAccepted)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	cmd := NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetIn(strings.NewReader("no\n"))
	out, err := executeProjectCommand(t, cmd, "delete", projectAlphaID)
	require.NoError(t, err)
	assert.False(t, sawDelete)
	assert.Contains(t, out, "You are about to delete a resource permanently")
	assert.Contains(t, out, "Delete project '"+projectAlphaID+"'?")
	assert.Contains(t, out, "Delete cancelled.")
}

func executeProjectCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setProjectCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveProjectCommandTestConfig(t *testing.T, cfg *cliconfig.Config) {
	t.Helper()
	require.NoError(t, cfg.Save())
}

func loadProjectCommandTestConfig(t *testing.T) *cliconfig.Config {
	t.Helper()
	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	return cfg
}

func writeProjectCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func projectCommandPayload(id, name, status string, plan *string) map[string]any {
	payload := map[string]any{
		"id":               id,
		"name":             name,
		"status":           status,
		"all_regions":      true,
		"selected_regions": []string{},
		"created_at":       time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
		"updated_at":       time.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339),
	}
	if plan != nil {
		payload["plan"] = *plan
	}
	return payload
}

func TestProjectsKeysDefaultsToCurrentProject(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID, Name: "Beta"},
	})

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		gotPath = r.URL.Path
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				map[string]any{
					"id":         "33333333-3333-4333-8333-333333333333",
					"name":       "default",
					"key_value":  "ak-anon-jwt-value",
					"is_default": true,
				},
			},
		})
	}))
	defer server.Close()

	// No project-id arg: must target the currently selected project.
	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "keys")
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectBetaID+"/anon-keys", gotPath)
	for _, want := range []string{"default", "(default)", "ak-anon-jwt-value", "33333333-3333-4333-8333-333333333333"} {
		assert.Contains(t, out, want)
	}
}

func TestProjectsKeysExplicitIDAndEmpty(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{"data": []any{}})
	}))
	defer server.Close()

	// Explicit ID is used, and an empty key list renders a clear message (no current project needed).
	out, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "keys", projectAlphaID)
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectAlphaID+"/anon-keys", gotPath)
	assert.Contains(t, out, "No anon keys for this project.")
}
