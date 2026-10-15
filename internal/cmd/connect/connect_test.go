package connectcmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	connectProjectID      = "11111111-1111-4111-8111-111111111111"
	connectOtherProjectID = "22222222-2222-4222-8222-222222222222"
	connectAnonKey        = "ak-connect-default"
)

// fakeAPI serves the endpoints connect calls. Every project it knows starts
// active, so connect never waits.
type fakeAPI struct {
	t        *testing.T
	projects []map[string]any
	// createStatus and createError make POST /projects fail; 0 creates.
	createStatus int
	createError  string
	creates      int
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/projects":
		writeConnectJSON(f.t, w, http.StatusOK, map[string]any{
			"data": f.projects, "has_more": false, "page": 1, "limit": 100, "total": len(f.projects),
		})
	case r.Method == http.MethodPost && r.URL.Path == "/projects":
		f.creates++
		if f.createStatus != 0 {
			writeConnectJSON(f.t, w, f.createStatus, map[string]any{"error": f.createError})
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		project := connectProjectPayload(connectProjectID, body.Name)
		f.projects = append(f.projects, project)
		writeConnectJSON(f.t, w, http.StatusCreated, project)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/anon-keys"):
		writeConnectJSON(f.t, w, http.StatusOK, map[string]any{
			"data": []any{
				map[string]any{"id": "33333333-3333-4333-8333-333333333333", "name": "old", "key_value": "ak-old", "is_default": false},
				map[string]any{"id": "44444444-4444-4444-8444-444444444444", "name": "default", "key_value": connectAnonKey, "is_default": true},
			},
		})
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/projects/"):
		id := strings.TrimPrefix(r.URL.Path, "/projects/")
		for _, p := range f.projects {
			if p["id"] == id {
				writeConnectJSON(f.t, w, http.StatusOK, p)
				return
			}
		}
		writeConnectJSON(f.t, w, http.StatusNotFound, map[string]any{"error": "project not found"})
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func TestConnectCreatesAProjectNamedAfterTheDirectoryAndLinksIt(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "My App")

	out, err := runConnect(t, server, dir, "--json")
	require.NoError(t, err)

	var res result
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	assert.Equal(t, projectRef{ID: connectProjectID, Name: "my-app"}, res.Project)
	assert.True(t, res.Created)
	assert.Equal(t, 1, fake.creates)
	assert.Equal(t, "http://localhost:3000/dashboard/overview", res.DashboardURL)

	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, cliconfig.ProjectConfig{ID: connectProjectID, Name: "my-app"}, link.Project)

	env := readFile(t, filepath.Join(dir, "volcano", "cloud.env"))
	for _, line := range []string{
		"VOLCANO_API_URL=" + server.URL,
		"VOLCANO_ANON_KEY=" + connectAnonKey,
		"NEXT_PUBLIC_VOLCANO_API_URL=" + server.URL,
		"NEXT_PUBLIC_VOLCANO_ANON_KEY=" + connectAnonKey,
	} {
		assert.Contains(t, env, line+"\n")
	}
	info, err := os.Stat(filepath.Join(dir, "volcano", "cloud.env"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	assert.FileExists(t, filepath.Join(dir, "volcano", "volcano.env"), "the base scaffold is created")
	agents := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.Contains(t, agents, "linked to the Volcano cloud project my-app ("+connectProjectID+")")

	assertGitIgnored(t, dir, "volcano/cloud.env", ".volcano/project.json")
}

func TestConnectRerunReusesTheLinkAndUpdatesFilesInPlace(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectProjectID, "shop")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := t.TempDir()
	_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: connectProjectID, Name: "shop"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# My rules\n\nUse tabs.\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "cloud.env"), []byte("STRIPE_KEY=sk_test\nVOLCANO_ANON_KEY=ak-stale\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("node_modules"), 0o644))

	for range 2 {
		_, err := runConnect(t, server, dir)
		require.NoError(t, err)
	}

	assert.Zero(t, fake.creates, "a linked directory keeps its project")
	env := readFile(t, filepath.Join(dir, "volcano", "cloud.env"))
	assert.Contains(t, env, "STRIPE_KEY=sk_test\n")
	assert.Equal(t, 1, strings.Count(env, "\nVOLCANO_ANON_KEY="), env)
	assert.Contains(t, env, "VOLCANO_ANON_KEY="+connectAnonKey+"\n")
	assert.NotContains(t, env, "ak-stale")

	agents := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.True(t, strings.HasPrefix(agents, "# My rules\n\nUse tabs.\n\n"), agents)
	assert.Equal(t, 1, strings.Count(agents, agentsStartMarker), agents)

	gitignore := readFile(t, filepath.Join(dir, ".gitignore"))
	assert.True(t, strings.HasPrefix(gitignore, "node_modules\n"), gitignore)
	assert.Equal(t, 1, strings.Count(gitignore, ".volcano/project.json"), gitignore)
	assert.NoDirExists(t, filepath.Join(dir, "volcano", "migrations"), "an existing volcano/ directory is left alone")
}

// The free plan allows one project and signup creates it, so a new user's
// create always hits the limit. connect links that project instead.
func TestConnectAtTheProjectLimitLinksTheOnlyProject(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{
		t:            t,
		projects:     []map[string]any{connectProjectPayload(connectOtherProjectID, "first-project")},
		createStatus: http.StatusForbidden,
		createError:  "project limit exceeded (max 1 projects per user)",
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := t.TempDir()

	out, err := runConnect(t, server, dir, "--json")
	require.NoError(t, err)

	var res result
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	assert.Equal(t, projectRef{ID: connectOtherProjectID, Name: "first-project"}, res.Project)
	assert.False(t, res.Created)
}

func TestConnectAtTheProjectLimitWithSeveralProjectsNamesThem(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{
		t: t,
		projects: []map[string]any{
			connectProjectPayload(connectProjectID, "shop"),
			connectProjectPayload(connectOtherProjectID, "blog"),
		},
		createStatus: http.StatusForbidden,
		createError:  "project limit exceeded (max 2 projects per user)",
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	_, err := runConnect(t, server, t.TempDir(), "--json")
	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, api.Status(err))
	require.ErrorContains(t, err, "Pass --project with one of your projects")
	require.ErrorContains(t, err, "shop ("+connectProjectID+")")
	require.ErrorContains(t, err, "blog ("+connectOtherProjectID+")")
}

func TestConnectWithProjectLinksIt(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := t.TempDir()

	_, err := runConnect(t, server, dir, "--project", "blog")
	require.NoError(t, err)

	assert.Zero(t, fake.creates)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectOtherProjectID, link.Project.ID)
}

func TestDefaultProjectName(t *testing.T) {
	assert.Equal(t, "my-app", defaultProjectName("/tmp/My App"))
	assert.Equal(t, "shop-v2", defaultProjectName("/tmp/shop_v2"))
	assert.Equal(t, "my-app", defaultProjectName("/tmp/_"))
}

func runConnect(t *testing.T, server *httptest.Server, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(append([]string{dir, "--skip-skills"}, args...))
	err := cmd.Execute()
	return stdout.String(), err
}

func setConnectTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{"VOLCANO_TOKEN", "VOLCANO_PROJECT_ID", "VOLCANO_API_URL", "VOLCANO_WEB_URL"} {
		t.Setenv(key, "")
	}
	require.NoError(t, (&cliconfig.Config{UserToken: "pk-connect"}).Save())
}

func connectProjectPayload(id, name string) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	return map[string]any{
		"id": id, "name": name, "status": "active",
		"all_regions": true, "selected_regions": []string{},
		"created_at": now, "updated_at": now,
	}
}

func writeConnectJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func assertGitIgnored(t *testing.T, dir string, paths ...string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	require.NoError(t, exec.Command("git", "-C", dir, "init", "-q").Run())
	for _, path := range paths {
		out, err := exec.Command("git", "-C", dir, "check-ignore", path).CombinedOutput()
		assert.NoError(t, err, "%s is not gitignored: %s", path, out)
	}
}
