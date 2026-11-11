package cloud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	flagProjectAlphaID = "11111111-1111-4111-8111-111111111111"
	flagProjectBetaID  = "22222222-2222-4222-8222-222222222222"
)

// projectFlagServer answers project lookups for Alpha, Beta, and the saved
// Gamma project, and records which project each variables list ran against.
type projectFlagServer struct {
	*httptest.Server
	mu        sync.Mutex
	listedFor []string
}

func newProjectFlagServer(t *testing.T) *projectFlagServer {
	t.Helper()
	projects := map[string]string{
		flagProjectAlphaID: "Alpha",
		flagProjectBetaID:  "Beta",
		cloudProjectID:     "Gamma",
	}
	s := &projectFlagServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/projects":
			data := make([]any, 0, len(projects))
			for _, id := range []string{flagProjectAlphaID, flagProjectBetaID, cloudProjectID} {
				data = append(data, flagProjectPayload(id, projects[id]))
			}
			writeCloudCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": data, "has_more": false, "page": 1, "limit": 100, "total": len(data),
			})
		case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "projects":
			name, ok := projects[parts[1]]
			if !ok {
				writeCloudCommandJSON(t, w, http.StatusNotFound, map[string]any{"error": "project not found"})
				return
			}
			writeCloudCommandJSON(t, w, http.StatusOK, flagProjectPayload(parts[1], name))
		case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "projects" && parts[2] == "variables":
			s.mu.Lock()
			s.listedFor = append(s.listedFor, parts[1])
			s.mu.Unlock()
			writeCloudCommandJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{}, "has_more": false, "page": 1, "limit": 100, "total": 0,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *projectFlagServer) listed() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.listedFor...)
}

func (s *projectFlagServer) command() *cobra.Command {
	return New(cliruntime.Deps{HTTPClient: s.Client(), APIBaseURL: s.URL})
}

func flagProjectPayload(id, name string) map[string]any {
	return map[string]any{
		"id":               id,
		"name":             name,
		"status":           "active",
		"all_regions":      true,
		"selected_regions": []string{},
		"created_at":       "2026-05-20T00:00:00Z",
		"updated_at":       "2026-05-20T00:00:00Z",
	}
}

// --project picks the project for one command by ID or exact name, outranks
// VOLCANO_PROJECT_ID, the link, and the saved project, and changes none of
// them.
func TestCloudProjectFlagSelectsProjectForOneCommand(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
		link bool
		args []string
		want string
	}{
		{name: "saved project without the flag", want: cloudProjectID},
		{name: "ID", args: []string{"--project", flagProjectBetaID}, want: flagProjectBetaID},
		{name: "name", args: []string{"--project", "Alpha"}, want: flagProjectAlphaID},
		{name: "name before the subcommand", args: []string{"--project=Beta"}, want: flagProjectBetaID},
		{name: "over VOLCANO_PROJECT_ID", env: flagProjectAlphaID, args: []string{"--project", "Beta"}, want: flagProjectBetaID},
		{name: "over the link", link: true, args: []string{"--project", "Beta"}, want: flagProjectBetaID},
		{name: "link without the flag", link: true, want: flagProjectAlphaID},
	} {
		t.Run(test.name, func(t *testing.T) {
			setCloudCommandTestHome(t)
			saveCloudCommandTestConfig(t)
			dir := t.TempDir()
			t.Chdir(dir)
			if test.link {
				_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: flagProjectAlphaID, Name: "Alpha"})
				require.NoError(t, err)
			}
			t.Setenv("VOLCANO_PROJECT_ID", test.env)
			cliconfig.ConsumeProjectEnvUse()
			server := newProjectFlagServer(t)

			args := []string{"variables", "list"}
			if len(test.args) > 0 && strings.HasPrefix(test.args[0], "--project=") {
				args = append(test.args, args...)
			} else {
				args = append(args, test.args...)
			}
			_, err := executeCloudCommand(t, server.command(), args...)
			require.NoError(t, err)
			assert.Equal(t, []string{test.want}, server.listed())

			// The flag chose the project without reading the deprecated
			// variable, so the command prints no deprecation warning.
			assert.Equal(t, test.env != "" && len(test.args) == 0, cliconfig.ConsumeProjectEnvUse())

			saved, err := cliconfig.LoadWithoutLink()
			require.NoError(t, err)
			require.NotNil(t, saved.CurrentProject)
			assert.Equal(t, cloudProjectID, saved.CurrentProject.ID)
			if test.link {
				link, err := cliconfig.FindLink(dir)
				require.NoError(t, err)
				require.NotNil(t, link)
				assert.Equal(t, flagProjectAlphaID, link.Project.ID)
			}
		})
	}
}

// A project --project does not name fails the way `volcano use` does, before
// the command runs.
func TestCloudProjectFlagRejectsUnknownProject(t *testing.T) {
	for _, test := range []struct {
		name       string
		identifier string
		want       string
	}{
		{name: "name", identifier: "Nope", want: "project not found: Nope"},
		{name: "ID", identifier: "99999999-9999-4999-8999-999999999999", want: "project not found: 99999999-9999-4999-8999-999999999999"},
	} {
		t.Run(test.name, func(t *testing.T) {
			setCloudCommandTestHome(t)
			saveCloudCommandTestConfig(t)
			t.Chdir(t.TempDir())
			server := newProjectFlagServer(t)

			_, err := executeCloudCommand(t, server.command(), "variables", "list", "--project", test.identifier)
			require.ErrorIs(t, err, cliproject.ErrNotFound)
			assert.EqualError(t, err, test.want)
			assert.Empty(t, server.listed())
		})
	}
}

func TestCloudProjectFlagRejectsEmptyValue(t *testing.T) {
	setCloudCommandTestHome(t)
	saveCloudCommandTestConfig(t)
	t.Chdir(t.TempDir())
	server := newProjectFlagServer(t)

	_, err := executeCloudCommand(t, server.command(), "variables", "list", "--project", " ")
	require.EqualError(t, err, `invalid argument " " for "--project" flag: give a project name or ID`)
	assert.Empty(t, server.listed())
}

// A project access token reaches its own project by ID, but cannot look a
// project up by name; that refusal matches `volcano use`.
func TestCloudProjectFlagNameNeedsAccountToken(t *testing.T) {
	setCloudCommandTestHome(t)
	t.Chdir(t.TempDir())
	require.NoError(t, (&cliconfig.Config{UserToken: "pt-project-token"}).Save())
	server := newProjectFlagServer(t)

	_, err := executeCloudCommand(t, server.command(), "variables", "list", "--project", "Beta")
	require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired)
	assert.Empty(t, server.listed())

	_, err = executeCloudCommand(t, server.command(), "variables", "list", "--project", flagProjectBetaID)
	require.NoError(t, err)
	assert.Equal(t, []string{flagProjectBetaID}, server.listed())
}

func TestCloudProjectFlagInHelp(t *testing.T) {
	out, err := executeCloudCommand(t, New(cliruntime.Deps{}), "lavaflow", "status", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "--project string")
	assert.Contains(t, out, projectFlagUsage)
}

// Cobra runs only the nearest PersistentPreRun, so a cloud subcommand that set
// its own would silently skip resolving --project.
func TestCloudSubcommandsKeepProjectFlagHook(t *testing.T) {
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			assert.Nil(t, child.PersistentPreRun, child.CommandPath())
			assert.Nil(t, child.PersistentPreRunE, child.CommandPath())
			assert.Nil(t, child.Flags().Lookup(projectFlagName), child.CommandPath())
			walk(child)
		}
	}
	cloud := New(cliruntime.Deps{})
	require.NotNil(t, cloud.PersistentPreRunE)
	walk(cloud)
}
