package deploylink

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	projectID = "22222222-2222-4222-8222-222222222222"
	linkID    = "k3Vn8TqPz0aX"
	linksPath = "/projects/" + projectID + "/deploy-links"
)

func TestCreateSendsOnlyTheFlagsGivenAndPrintsTheSnippets(t *testing.T) {
	setTestHome(t)
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, linksPath, r.URL.Path)
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		var err error
		body, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		respondJSON(t, w, http.StatusCreated, linkPayload("active"))
	}))
	defer server.Close()

	out, err := executeCommand(t, newCommand(server), "create", "--path", "apps/web", "--name", "my-app")
	require.NoError(t, err)

	assert.JSONEq(t, `{"path":"apps/web","suggested_name":"my-app"}`, string(body))
	for _, want := range []string{
		"Created deploy link " + linkID,
		"URL: https://volcano.dev/deploy/" + linkID,
		"[![Deploy to Volcano](https://volcano.dev/deploy-button.svg)](https://volcano.dev/deploy/" + linkID + ")",
		`<a href="https://volcano.dev/deploy/` + linkID + `"><img src="https://volcano.dev/deploy-button.svg" alt="Deploy to Volcano"></a>`,
	} {
		assert.Contains(t, out, want)
	}
}

func TestCreateSendsTheBranch(t *testing.T) {
	setTestHome(t)
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		respondJSON(t, w, http.StatusCreated, linkPayload("active"))
	}))
	defer server.Close()

	out, err := executeCommand(t, newCommand(server), "create", "--branch", "release", "--json")
	require.NoError(t, err)

	assert.JSONEq(t, `{"branch":"release"}`, string(body))
	var link map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &link), "--json prints only the link: %q", out)
	assert.Equal(t, linkID, link["id"])
}

// Each documented refusal names what to do, and no server text beyond the
// error field reaches the user.
func TestCreateExplainsTheDocumentedRefusals(t *testing.T) {
	for _, tc := range []struct {
		status  int
		message string
		want    []string
	}{
		{http.StatusConflict, "no repository is connected", []string{"volcano git connect", "volcano git status", "20"}},
		{http.StatusUnprocessableEntity, "the repository is private", []string{"public repository", "push to"}},
		{http.StatusUnauthorized, "the GitHub connection expired", []string{"reconnect GitHub", "volcano login"}},
		{http.StatusServiceUnavailable, "GitHub is unavailable", []string{"GitHub is not available right now"}},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			setTestHome(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				respondJSON(t, w, tc.status, map[string]any{
					"error": tc.message, "detail": "SECRET-INTERNAL-DETAIL",
				})
			}))
			defer server.Close()

			_, err := executeCommand(t, newCommand(server), "create")
			require.Error(t, err)
			assert.Equal(t, tc.status, api.Status(err))
			assert.Contains(t, err.Error(), fmt.Sprintf("HTTP %d: %s", tc.status, tc.message))
			for _, want := range tc.want {
				assert.Contains(t, err.Error(), want)
			}
			assert.NotContains(t, err.Error(), "SECRET-INTERNAL-DETAIL")
			assert.NotContains(t, err.Error(), "volcano cloud git", "git and login are not in the cloud tree")
		})
	}
}

func TestListShowsLinksAndAnEmptyProject(t *testing.T) {
	setTestHome(t)
	links := []any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, linksPath, r.URL.Path)
		respondJSON(t, w, http.StatusOK, map[string]any{"deploy_links": links})
	}))
	defer server.Close()

	out, err := executeCommand(t, newCommand(server), "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No deploy links found")

	out, err = executeCommand(t, newCommand(server), "list", "--json")
	require.NoError(t, err)
	assert.JSONEq(t, `[]`, out, "an empty project is an empty array, not null")

	links = []any{linkPayload("active")}
	out, err = executeCommand(t, newCommand(server), "list")
	require.NoError(t, err)
	for _, want := range []string{linkID, "acme/notes-app", "main", "active"} {
		assert.Contains(t, out, want)
	}
}

func TestGetShowsTheLinkAndItsDisabledReason(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, linksPath+"/"+linkID, r.URL.Path)
		payload := linkPayload("disabled")
		payload["disabled_reason"] = "repository_private"
		respondJSON(t, w, http.StatusOK, payload)
	}))
	defer server.Close()

	out, err := executeCommand(t, newCommand(server), "get", linkID)
	require.NoError(t, err)
	for _, want := range []string{"Status: disabled", "Disabled reason: repository_private", "URL: https://volcano.dev/deploy/" + linkID} {
		assert.Contains(t, out, want)
	}

	out, err = executeCommand(t, newCommand(server), "get", linkID, "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"disabled_reason": "repository_private"`)
}

func TestGetReportsAnUnknownLink(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusNotFound, map[string]any{"error": "deploy link not found"})
	}))
	defer server.Close()

	_, err := executeCommand(t, newCommand(server), "get", "missing")
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, api.Status(err))
	assert.Contains(t, err.Error(), `failed to get deploy link "missing"`)
}

func TestStatsPrintsTotalsAndDaysAndSendsTheWindow(t *testing.T) {
	setTestHome(t)
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, linksPath+"/"+linkID+"/stats", r.URL.Path)
		query = r.URL.RawQuery
		respondJSON(t, w, http.StatusOK, map[string]any{
			"since": "2026-09-19T21:20:00Z",
			"totals": []any{
				map[string]any{"kind": "click", "events": 42, "visitors": 31},
				map[string]any{"kind": "confirm", "events": 9, "visitors": 9},
				map[string]any{"kind": "ready", "events": 8, "visitors": 0},
			},
			"daily": []any{map[string]any{"day": "2026-10-18", "kind": "click", "events": 17}},
		})
	}))
	defer server.Close()

	out, err := executeCommand(t, newCommand(server), "stats", linkID)
	require.NoError(t, err)
	assert.Empty(t, query, "no --days leaves the window to the API")
	for _, want := range []string{"Kind", "click", "42", "31", "confirm", "ready", "Day", "2026-10-18", "17"} {
		assert.Contains(t, out, want)
	}

	out, err = executeCommand(t, newCommand(server), "stats", linkID, "--days", "7", "--json")
	require.NoError(t, err)
	assert.Equal(t, "days=7", query)
	assert.Contains(t, out, `"totals"`)
}

func TestStatsRefusesADaysWindowThatIsNotPositive(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request is sent for an invalid --days")
	}))
	defer server.Close()

	for _, days := range []string{"0", "-3"} {
		_, err := executeCommand(t, newCommand(server), "stats", linkID, "--days", days)
		require.EqualError(t, err, "--days must be at least 1")
	}
}

func TestDisableAndEnablePatchTheStatus(t *testing.T) {
	for _, tc := range []struct {
		command, status, want string
	}{
		{"disable", "disabled", "Disabled deploy link " + linkID},
		{"enable", "active", "Enabled deploy link " + linkID},
	} {
		t.Run(tc.command, func(t *testing.T) {
			setTestHome(t)
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPatch, r.Method)
				assert.Equal(t, linksPath+"/"+linkID, r.URL.Path)
				body, _ = io.ReadAll(r.Body)
				respondJSON(t, w, http.StatusOK, linkPayload(tc.status))
			}))
			defer server.Close()

			out, err := executeCommand(t, newCommand(server), tc.command, linkID)
			require.NoError(t, err)
			assert.JSONEq(t, `{"status":"`+tc.status+`"}`, string(body))
			assert.Contains(t, out, tc.want)
		})
	}
}

func TestEnableExplainsAVerificationRefusal(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(t, w, http.StatusUnprocessableEntity, map[string]any{"error": "the repository is private"})
	}))
	defer server.Close()

	_, err := executeCommand(t, newCommand(server), "enable", linkID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to enable deploy link "`+linkID+`"`)
	assert.Contains(t, err.Error(), "the repository is private")
}

func TestDeleteAsksFirstUnlessYes(t *testing.T) {
	setTestHome(t)
	deleted := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, linksPath+"/"+linkID, r.URL.Path)
		deleted++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	out, err := executeCommandWithInput(t, newCommand(server), "n\n", "delete", linkID)
	require.NoError(t, err)
	assert.Zero(t, deleted, "a decline deletes nothing")
	assert.Contains(t, out, "Delete deploy link '"+linkID+"'?")

	out, err = executeCommandWithInput(t, newCommand(server), "y\n", "delete", linkID)
	require.NoError(t, err)
	assert.Equal(t, 1, deleted)
	assert.Contains(t, out, "Deleted deploy link "+linkID)

	out, err = executeCommandWithInput(t, newCommand(server), "", "delete", linkID, "--yes")
	require.NoError(t, err)
	assert.Equal(t, 2, deleted)
	assert.NotContains(t, out, "Type 'y'")
}

func TestAnEmptyIDIsRefusedBeforeAnythingIsSent(t *testing.T) {
	setTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("no request is sent for an empty ID")
	}))
	defer server.Close()

	for _, command := range []string{"get", "stats", "disable", "enable", "delete"} {
		_, err := executeCommand(t, newCommand(server), command, "  ")
		require.EqualError(t, err, "the deploy link ID is empty", command)
	}
}

func TestCloudOnlyStubRefusesAndStillShowsHelp(t *testing.T) {
	cmd := NewCloudOnly()
	assert.True(t, cmd.Hidden)

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"create", "--branch", "main"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"deploy-link" is a cloud command`)
	assert.Contains(t, err.Error(), "volcano cloud deploy-link")

	out.Reset()
	cmd = NewCloudOnly()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "cloud-only")
}

func TestGroupListsEverySubcommand(t *testing.T) {
	var names []string
	for _, sub := range New(cliruntime.Deps{}).Commands() {
		names = append(names, sub.Name())
	}
	assert.ElementsMatch(t, []string{"create", "list", "get", "stats", "disable", "enable", "delete"}, names)
}

func newCommand(server *httptest.Server) *cobra.Command {
	return New(cliruntime.Deps{
		CommandPathPrefix: "volcano cloud",
		HTTPClient:        server.Client(),
		APIBaseURL:        server.URL,
	})
}

func executeCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	return executeCommandWithInput(t, cmd, "", args...)
}

func executeCommandWithInput(t *testing.T, cmd *cobra.Command, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString(stdin))
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

	cfg := &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: projectID, Name: "Beta"},
	}
	require.NoError(t, cfg.Save())
}

func respondJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func linkPayload(status string) map[string]any {
	return map[string]any{
		"id":                    linkID,
		"project_id":            projectID,
		"repo_id":               123456789,
		"repo_full_name":        "acme/notes-app",
		"branch":                "main",
		"path":                  "apps/web",
		"suggested_name":        "my-app",
		"status":                status,
		"last_validated_at":     "2026-10-19T21:12:00Z",
		"last_validated_commit": "0123456789abcdef0123456789abcdef01234567",
		"created_at":            "2026-10-19T21:12:00Z",
		"updated_at":            "2026-10-19T21:12:00Z",
	}
}
