package project

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestUseByNameAndProjectCreateRenameGetDelete(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
	var listRequests int
	var createRequests int
	var getRequests int
	var renameRequests int
	var deleteRequests int
	var createPayload map[string]any
	var renamePayload map[string]any
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
		case r.Method == http.MethodPatch && r.URL.Path == "/projects/"+projectBetaID:
			renameRequests++
			require.NoError(t, json.NewDecoder(r.Body).Decode(&renamePayload))
			writeProjectCommandJSON(t, w, http.StatusOK, projectCommandPayload(projectBetaID, "Gamma", "active", nil))
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

	out, err = executeProjectCommand(t, NewProjects(deps), "rename", projectBetaID, " Gamma ")
	require.NoError(t, err)
	assert.Contains(t, out, "Project renamed: Gamma ("+projectBetaID+")")
	assert.Equal(t, map[string]any{"name": "Gamma"}, renamePayload)
	assert.Equal(t, 1, renameRequests)
	cfg = loadProjectCommandTestConfig(t)
	require.NotNil(t, cfg.CurrentProject)
	assert.Equal(t, "Gamma", cfg.CurrentProject.Name)

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

// `volcano projects delete <id> </dev/null` used to print "Delete cancelled."
// and exit 0 with the project intact, which a script cannot tell apart from a
// deletion that happened.
func TestProjectDeleteRefusesWhenStdinCannotAnswer(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request without confirmation", r.URL.Path)
	}))
	defer server.Close()

	closed, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closed.Close() })

	cmd := NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetIn(closed)
	out, err := executeProjectCommand(t, cmd, "delete", projectAlphaID)

	require.ErrorContains(t, err, "confirmation required; pass --yes")
	assert.NotContains(t, out, "Delete cancelled.", "a prompt nobody can answer is not a cancellation")
}

// Deleting and renaming a project are account operations the platform refuses a
// pt- token on. Delete asked for confirmation first, so the user agreed to a
// destructive action the CLI already knew would fail.
func TestProjectsDeleteAndRenameRejectAProjectToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request with a project access token", r.URL.Path)
	}))
	defer server.Close()

	for _, args := range [][]string{
		{"delete", projectAlphaID},
		{"delete", projectAlphaID, "--yes"},
		{"rename", projectAlphaID, "Renamed"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			setProjectCommandTestHome(t)
			saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: cliconfig.ProjectTokenPrefix + "token"})

			cmd := NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
			cmd.SetIn(strings.NewReader("y\n"))
			out, err := executeProjectCommand(t, cmd, args...)

			require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired)
			assert.NotContains(t, out, "You are about to delete a resource permanently",
				"the refusal must come before the confirmation prompt")
		})
	}
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

func TestProjectsKeysHonorsEnvProjectPrecedence(t *testing.T) {
	setProjectCommandTestHome(t)
	// Saved current project is Beta, but VOLCANO_PROJECT_ID selects Alpha and must win.
	saveProjectCommandTestConfig(t, &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectBetaID, Name: "Beta"},
	})
	t.Setenv("VOLCANO_PROJECT_ID", projectAlphaID)

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{"data": []any{}})
	}))
	defer server.Close()

	_, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "keys")
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectAlphaID+"/anon-keys", gotPath)
}

// `export VOLCANO_PROJECT_ID=$(cat project-id)` carries a trailing newline, the
// way the CI docs have users set it. Untrimmed it reached uuid.Parse and failed
// as "invalid UUID length: 37".
func TestProjectsKeysTrimsTheEnvProject(t *testing.T) {
	setProjectCommandTestHome(t)
	saveProjectCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})
	t.Setenv("VOLCANO_PROJECT_ID", projectAlphaID+"\n")

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeProjectCommandJSON(t, w, http.StatusOK, map[string]any{"data": []any{}})
	}))
	defer server.Close()

	_, err := executeProjectCommand(t, NewProjects(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "keys")
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectAlphaID+"/anon-keys", gotPath)
}
