package connectcmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// takenNames answer POST /projects with 409 for these names.
	takenNames map[string]bool
	creates    int
	// createAuth is the Authorization header of the last POST /projects.
	createAuth string
	// exchangeStatus and exchangeCode refuse the bootstrap token exchange; 0
	// exchanges it for pk-exchanged.
	exchangeStatus int
	exchangeCode   string
	exchanges      int
	// listStatus makes GET /projects fail; 0 lists f.projects.
	listStatus int
	// deviceAuthorize records that browser login started. The fake refuses it.
	deviceAuthorize bool
	// anonKeyPages, when set, replace the default anon-key list, one entry per
	// page.
	anonKeyPages [][]any
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/projects" && f.listStatus != 0:
		writeConnectJSON(f.t, w, f.listStatus, map[string]any{"error": "failed"})
	case r.Method == http.MethodGet && r.URL.Path == "/projects":
		writeConnectJSON(f.t, w, http.StatusOK, map[string]any{
			"data": f.projects, "has_more": false, "page": 1, "limit": 100, "total": len(f.projects),
		})
	case r.Method == http.MethodPost && r.URL.Path == "/auth/device/authorize":
		f.deviceAuthorize = true
		writeConnectJSON(f.t, w, http.StatusServiceUnavailable, map[string]any{"error": "unavailable"})
	case r.Method == http.MethodPost && r.URL.Path == "/auth/cli-bootstrap-tokens/exchange":
		f.exchanges++
		if f.exchangeStatus != 0 {
			writeConnectJSON(f.t, w, f.exchangeStatus, map[string]any{"error": "refused", "code": f.exchangeCode})
			return
		}
		writeConnectJSON(f.t, w, http.StatusOK, map[string]any{"token": "pk-exchanged", "user_id": "user-1", "token_id": "tok-1"})
	case r.Method == http.MethodPost && r.URL.Path == "/projects":
		f.creates++
		f.createAuth = r.Header.Get("Authorization")
		if f.createStatus != 0 {
			writeConnectJSON(f.t, w, f.createStatus, map[string]any{"error": f.createError})
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		if f.takenNames[body.Name] {
			writeConnectJSON(f.t, w, http.StatusConflict, map[string]any{"error": "project name already exists"})
			return
		}
		project := connectProjectPayload(connectProjectID, body.Name)
		f.projects = append(f.projects, project)
		writeConnectJSON(f.t, w, http.StatusCreated, project)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/anon-keys") && f.anonKeyPages != nil:
		page := 1
		if raw := r.URL.Query().Get("page"); raw != "" {
			var err error
			page, err = strconv.Atoi(raw)
			require.NoError(f.t, err)
		}
		writeConnectJSON(f.t, w, http.StatusOK, map[string]any{
			"data": f.anonKeyPages[page-1], "has_more": page < len(f.anonKeyPages),
		})
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
	assert.Equal(t, "http://localhost:3000/dashboard/overview?project="+connectProjectID, res.DashboardURL)

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
	assert.Contains(t, agents, "linked to a Volcano cloud project in .volcano/project.json")
	assert.Contains(t, agents, "If the file is\nmissing, run `volcano connect .` here.", "a bare connect would set up a new directory instead")
	assert.NotContains(t, agents, connectProjectID, "AGENTS.md is shared; the project is each developer's own")

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
	// No scaffold ran, so only the root .gitignore entries connect adds keep these out.
	assertGitIgnored(t, dir, "volcano/cloud.env", ".volcano/project.json")
}

// connect always creates a project. At the plan's limit it fails and names
// the projects that --project could link instead.
func TestConnectAtTheProjectLimitNamesTheProjectsToLink(t *testing.T) {
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

	_, err := runConnect(t, server, dir, "--json")
	require.Error(t, err)
	assert.Equal(t, http.StatusForbidden, api.Status(err))
	require.ErrorContains(t, err, "project limit exceeded")
	require.ErrorContains(t, err, "first-project: volcano connect --project "+connectOtherProjectID)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	assert.Nil(t, link, "nothing is linked when no project was created")
}

func TestConnectAddsASuffixWhenTheNameIsTaken(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, takenNames: map[string]bool{"shop": true, "shop-2": true}}
	server := httptest.NewServer(fake)
	defer server.Close()

	out, err := runConnect(t, server, filepath.Join(t.TempDir(), "shop"), "--json")
	require.NoError(t, err)

	var res result
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	assert.Equal(t, "shop-3", res.Project.Name)
	assert.True(t, res.Created)
	assert.Equal(t, 3, fake.creates)
}

func TestConnectRejectsAnUnknownTemplateBeforeCreatingAProject(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()

	_, err := runConnect(t, server, t.TempDir(), "--template", "nextjss")
	require.ErrorContains(t, err, `unknown starter "nextjss"`)
	assert.Zero(t, fake.creates)
}

// connect's first run wrote a root .gitignore that differs from the starter's.
// A later --template still adds the starter.
func TestConnectAddsATemplateToAConnectedDirectory(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "shop")

	_, err := runConnect(t, server, dir)
	require.NoError(t, err)
	for range 2 {
		out, err := runConnect(t, server, dir, "--template", "nextjs")
		require.NoError(t, err)
		assert.NotContains(t, out, "Skipped the scaffold")
	}

	assert.Equal(t, 1, fake.creates)
	assert.FileExists(t, filepath.Join(dir, "package.json"))
	assert.FileExists(t, filepath.Join(dir, ".volcano", "source-layout.json"))
	gitignore := readFile(t, filepath.Join(dir, ".gitignore"))
	for _, entry := range []string{"node_modules", ".volcano/project.json", "volcano/*.env"} {
		assert.Equal(t, 1, strings.Count(gitignore, entry+"\n"), gitignore)
	}
}

// A --template that can't be added fails before sign-in, instead of
// connecting without the starter.
func TestConnectRefusesATemplateThatConflictsBeforeSigningIn(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644))

	_, err := runConnect(t, server, dir, "--template", "nextjs", "--token", "vbt_one-time")
	require.ErrorContains(t, err, "can't add the nextjs starter")
	require.ErrorContains(t, err, "package.json (has different content)")
	assert.Zero(t, fake.exchanges, "the one-time token is not spent")
	assert.Zero(t, fake.creates)
	assert.NoDirExists(t, filepath.Join(dir, ".volcano"))
}

// --name replaces a link connect can't use with a new project, so the check
// must come before the project is created.
func TestConnectWithNameRefusesATemplateThatConflictsBeforeCreatingAProject(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := chdirTemp(t)
	writeBadLink(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644))

	_, err := runConnectHere(t, server, "--name", "shop", "--template", "nextjs")
	require.ErrorContains(t, err, "can't add the nextjs starter")
	assert.Zero(t, fake.creates)
}

// With --project and no directory argument, the directory is known only once
// the project is: the check runs then, before anything is linked.
func TestConnectWithProjectRefusesATemplateThatConflicts(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(chdirTemp(t), "blog")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644))

	_, err := runConnectHere(t, server, "--project", connectOtherProjectID, "--template", "nextjs")
	require.ErrorContains(t, err, "can't add the nextjs starter")
	assert.NoDirExists(t, filepath.Join(dir, ".volcano"))
}

func TestConnectFindsTheDefaultAnonKeyOnALaterPage(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{
		t:        t,
		projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")},
		anonKeyPages: [][]any{
			{map[string]any{"id": "33333333-3333-4333-8333-333333333333", "name": "newest", "key_value": "ak-newest", "is_default": false}},
			{map[string]any{"id": "44444444-4444-4444-8444-444444444444", "name": "default", "key_value": connectAnonKey, "is_default": true}},
		},
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := t.TempDir()

	_, err := runConnect(t, server, dir, "--project", connectOtherProjectID)
	require.NoError(t, err)
	env := readFile(t, filepath.Join(dir, "volcano", "cloud.env"))
	assert.Contains(t, env, "VOLCANO_ANON_KEY="+connectAnonKey+"\n")
	assert.NotContains(t, env, "ak-newest")
}

func TestConnectProjectFlagTakesAnID(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()

	_, err := runConnect(t, server, t.TempDir(), "--project", "blog")
	require.ErrorContains(t, err, "--project takes a project ID")
	assert.Zero(t, fake.creates)
}

func TestConnectWithProjectLinksIt(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := t.TempDir()

	out, err := runConnect(t, server, dir, "--project", connectOtherProjectID)
	require.NoError(t, err)
	assert.Contains(t, out, "Project:   blog\n")
	assert.Contains(t, out, "Dashboard: http://localhost:3000/dashboard/overview?project="+connectOtherProjectID+"\n")

	assert.Zero(t, fake.creates)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectOtherProjectID, link.Project.ID)
}

func TestConnectWithNoDirectoryCreatesOneNamedAfterTheProject(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)

	out, err := runConnectHere(t, server)
	require.NoError(t, err)
	assert.Contains(t, out, "Project:   fuji-amber-42\nDirectory: ./fuji-amber-42\nDashboard: http://localhost:3000/dashboard/overview?project="+connectProjectID+"\n")
	assert.Contains(t, out, "Next: cd ./fuji-amber-42, read AGENTS.md there")

	dir := filepath.Join(parent, "fuji-amber-42")
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, cliconfig.ProjectConfig{ID: connectProjectID, Name: "fuji-amber-42"}, link.Project)
	assert.FileExists(t, filepath.Join(dir, "AGENTS.md"))
	assert.NoFileExists(t, filepath.Join(parent, "AGENTS.md"), "the working directory is left alone")
}

func TestConnectWithNoDirectorySkipsNamesWhoseDirectoryExists(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t, takenNames: map[string]bool{"fuji-amber-42-2": true}}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)
	require.NoError(t, os.Mkdir(filepath.Join(parent, "fuji-amber-42"), 0o755))

	out, err := runConnectHere(t, server, "--json")
	require.NoError(t, err)

	var res result
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	assert.Equal(t, "fuji-amber-42-3", res.Project.Name)
	assert.Equal(t, filepath.Join(parent, "fuji-amber-42-3"), res.Directory)
	assert.Equal(t, 2, fake.creates, "the existing directory's name is never sent")
}

func TestConnectWithNoDirectoryStopsWhenEveryNameHasADirectory(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "shop")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)
	require.NoError(t, os.Mkdir(filepath.Join(parent, "shop"), 0o755))
	for suffix := 2; suffix <= maxNameSuffix; suffix++ {
		require.NoError(t, os.Mkdir(filepath.Join(parent, fmt.Sprintf("shop-%d", suffix)), 0o755))
	}

	_, err := runConnectHere(t, server)
	require.ErrorContains(t, err, "shop through shop-20 are taken")
	assert.Zero(t, fake.creates, "an existing directory is never reused")
}

func TestConnectInsideALinkedDirectoryKeepsItsProject(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)

	_, err := runConnectHere(t, server)
	require.NoError(t, err)
	dir := filepath.Join(parent, "fuji-amber-42")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "web"), 0o755))
	t.Chdir(filepath.Join(dir, "web"))

	out, err := runConnectHere(t, server)
	require.NoError(t, err)
	assert.Equal(t, 1, fake.creates)
	assert.Contains(t, out, "Directory: "+dir+"\n")
	assert.NoDirExists(t, filepath.Join(dir, "web", "fuji-amber-42"))
}

const badLink = "<<<<<<< HEAD\n"

// writeBadLink gives dir a project link that can't be parsed and returns its
// path.
func writeBadLink(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, ".volcano", "project.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(badLink), 0o644))
	return path
}

// Replacing a bad link with a new project could leave the user with two, so
// connect asks which project goes there.
func TestConnectRefusesToGuessPastABadLink(t *testing.T) {
	for name, run := range map[string]func(*httptest.Server) (string, error){
		"directory argument": func(server *httptest.Server) (string, error) { return runConnect(t, server, ".") },
		"no directory":       func(server *httptest.Server) (string, error) { return runConnectHere(t, server) },
	} {
		t.Run(name, func(t *testing.T) {
			setConnectTestHome(t)
			fake := &fakeAPI{t: t}
			server := httptest.NewServer(fake)
			defer server.Close()
			dir := chdirTemp(t)
			path := writeBadLink(t, dir)
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "web"), 0o755))
			if name == "no directory" {
				t.Chdir(filepath.Join(dir, "web"))
			}

			_, err := run(server)
			require.ErrorContains(t, err, "failed to parse project link "+path)
			require.ErrorContains(t, err, "--project <project-id>")
			require.ErrorContains(t, err, "add --name <name>, or delete "+path)
			assert.Zero(t, fake.creates)
			assert.Equal(t, badLink, readFile(t, path))
		})
	}
}

// The refusal comes before sign-in, so it doesn't spend a one-time token the
// user needs for the rerun.
func TestConnectRefusesABadLinkBeforeSigningIn(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	path := writeBadLink(t, chdirTemp(t))

	_, err := runConnect(t, server, ".", "--token", "vbt_one-time")
	require.ErrorContains(t, err, "failed to parse project link "+path)
	assert.Zero(t, fake.exchanges)
	cfg, err := cliconfig.LoadWithoutLink()
	require.NoError(t, err)
	assert.Equal(t, "pk-connect", cfg.UserToken)
}

func TestConnectWithProjectReplacesABadLink(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := chdirTemp(t)
	path := writeBadLink(t, dir)

	out, err := runConnect(t, server, ".", "--project", connectOtherProjectID)
	require.NoError(t, err)
	assert.Contains(t, out, "Warning: Replacing a project link that can't be used: failed to parse project link "+path)
	assert.Zero(t, fake.creates)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectOtherProjectID, link.Project.ID)
}

func TestConnectWithNameReplacesABadLinkWithANewProject(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := chdirTemp(t)
	path := writeBadLink(t, dir)

	out, err := runConnectHere(t, server, "--name", "shop")
	require.NoError(t, err)
	assert.Contains(t, out, "Warning: Replacing a project link that can't be used: failed to parse project link "+path)
	assert.Equal(t, 1, fake.creates)
	assert.Contains(t, out, "Directory: .\n", "the directory that holds the link is the one set up")
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, cliconfig.ProjectConfig{ID: connectProjectID, Name: "shop"}, link.Project)
}

func TestConnectIgnoresABadLinkAboveTheDirectory(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)
	path := writeBadLink(t, parent)
	dir := filepath.Join(parent, "my-app")

	out, err := runConnect(t, server, dir)
	require.NoError(t, err)
	assert.Contains(t, out, "Warning: Ignoring a project link that can't be used: failed to parse project link "+path)
	assert.Equal(t, 1, fake.creates)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, dir, link.Dir)
	assert.Equal(t, badLink, readFile(t, path), "the parent's link is left alone")
}

func TestConnectWithProjectAndNoDirectoryUsesTheProjectName(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)

	out, err := runConnectHere(t, server, "--project", connectOtherProjectID)
	require.NoError(t, err)
	assert.Contains(t, out, "Directory: ./blog\n")
	assert.Zero(t, fake.creates)
	link, err := cliconfig.FindLink(filepath.Join(parent, "blog"))
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectOtherProjectID, link.Project.ID)
}

// The first run says to cd into the new directory. Rerunning the same command
// there sets up that directory instead of a ./blog inside it.
func TestConnectWithProjectRerunInsideItsDirectoryReusesIt(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(chdirTemp(t), "blog")

	_, err := runConnectHere(t, server, "--project", connectOtherProjectID)
	require.NoError(t, err)
	t.Chdir(dir)

	out, err := runConnectHere(t, server, "--project", connectOtherProjectID)
	require.NoError(t, err)
	assert.Contains(t, out, "Directory: .\n")
	assert.NoDirExists(t, filepath.Join(dir, "blog"))
}

func TestConnectWithProjectAndNoDirectoryKeepsAnotherProjectsDirectory(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{
		connectProjectPayload(connectProjectID, "my_app"),
		connectProjectPayload(connectOtherProjectID, "my-app"),
	}}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)

	// A rerun for the same project reuses its directory.
	for range 2 {
		_, err := runConnectHere(t, server, "--project", connectProjectID)
		require.NoError(t, err)
	}

	// my-app makes the same directory name as my_app, which is linked to it.
	_, err := runConnectHere(t, server, "--project", connectOtherProjectID)
	require.ErrorContains(t, err, "./my-app is linked to another project")
	link, err := cliconfig.FindLink(filepath.Join(parent, "my-app"))
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectProjectID, link.Project.ID, "the link is unchanged")
}

func TestConnectLeavesNoDirectoryWhenTheProjectIsNotCreated(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, createStatus: http.StatusForbidden, createError: "project limit exceeded (max 1 projects per user)"}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "my-app")

	_, err := runConnect(t, server, dir)
	require.ErrorContains(t, err, "project limit exceeded")
	assert.NoDirExists(t, dir)
}

func TestConnectWithABootstrapTokenSignsInWithTheExchangedLogin(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	out, err := runConnectHere(t, server, "--token", "vbt_one-time")
	require.NoError(t, err)
	assert.Contains(t, out, "Signed in with the token")
	assert.Equal(t, "Bearer pk-exchanged", fake.createAuth)

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-exchanged", cfg.UserToken)
	assert.Equal(t, "user-1", cfg.UserID)
}

func TestConnectSavesTheExchangedLoginWhenProjectIsWrong(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	_, err := runConnectHere(t, server, "--token", "vbt_one-time", "--project", connectOtherProjectID)
	require.ErrorContains(t, err, "failed to get project "+connectOtherProjectID)

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-exchanged", cfg.UserToken, "the one-time token's login is kept for the next run")
	assert.Nil(t, cfg.CurrentProject, "connect leaves the volcano use selection alone")
}

func TestConnectSavesTheExchangedLoginWhenItsCheckFails(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, listStatus: http.StatusInternalServerError}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	out, err := runConnectHere(t, server, "--token", "vbt_one-time")
	require.ErrorContains(t, err, "token authentication failed")
	assert.Contains(t, out, "The one-time token is spent, but its login is saved. Run the command again without --token.")
	assert.Zero(t, fake.creates)

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-exchanged", cfg.UserToken, "the one-time token's login is kept for the next run")
}

func TestConnectRefusesATokenThatVolcanoTokenWouldOverride(t *testing.T) {
	setConnectTestHome(t)
	t.Setenv("VOLCANO_TOKEN", "pk-from-env")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()

	for _, flags := range [][]string{{"--token", "vbt_one-time"}, {"--signup", "--email", "a@example.com"}} {
		_, err := runConnect(t, server, t.TempDir(), flags...)
		require.ErrorContains(t, err, "VOLCANO_TOKEN is set", flags)
	}
	assert.Zero(t, fake.exchanges, "the one-time token is not spent")
	assert.Zero(t, fake.creates)
	assert.False(t, fake.deviceAuthorize)
	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-connect", cfg.UserToken, "the saved login is unchanged")
}

// Every command run in the directory would use the project VOLCANO_PROJECT_ID
// names, not the one connect links, so connect refuses before signing in.
func TestConnectRefusesAProjectThatVolcanoProjectIDWouldOverride(t *testing.T) {
	for name, tc := range map[string]struct {
		args   []string
		linked bool
		want   string
	}{
		"another --project": {
			args: []string{"--project", connectProjectID},
			want: "Unset it, or set it to " + connectProjectID + ", and try again",
		},
		"a new project": {
			want: "Unset it, or pass --project " + connectOtherProjectID + " to set up that project, and try again",
		},
		"a directory linked to another project": {
			linked: true,
			want:   "Unset it, or set it to " + connectProjectID + ", and try again",
		},
	} {
		t.Run(name, func(t *testing.T) {
			setConnectTestHome(t)
			t.Setenv("VOLCANO_PROJECT_ID", connectOtherProjectID)
			fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectProjectID, "shop")}}
			server := httptest.NewServer(fake)
			defer server.Close()
			dir := filepath.Join(chdirTemp(t), "shop")
			if tc.linked {
				_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: connectProjectID, Name: "shop"})
				require.NoError(t, err)
			}

			_, err := runConnect(t, server, dir, append([]string{"--token", "vbt_one-time"}, tc.args...)...)
			require.ErrorContains(t, err, "VOLCANO_PROJECT_ID is set to "+connectOtherProjectID)
			require.ErrorContains(t, err, tc.want)
			assert.Zero(t, fake.exchanges, "the one-time token is not spent")
			assert.Zero(t, fake.creates)
			assert.NoFileExists(t, filepath.Join(dir, "volcano", "cloud.env"))
			if !tc.linked {
				assert.NoDirExists(t, dir)
			}
		})
	}
}

func TestConnectUsesTheProjectVolcanoProjectIDNames(t *testing.T) {
	setConnectTestHome(t)
	t.Setenv("VOLCANO_PROJECT_ID", connectProjectID)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectProjectID, "shop")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(chdirTemp(t), "shop")

	// The first run links it with --project; the rerun keeps the link.
	_, err := runConnect(t, server, dir, "--project", connectProjectID)
	require.NoError(t, err)
	_, err = runConnect(t, server, dir)
	require.NoError(t, err)
	assert.Zero(t, fake.creates)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectProjectID, link.Project.ID)
}

// The linked project is gone, so connect would create a new one, which
// VOLCANO_PROJECT_ID can't name.
func TestConnectRefusesToReplaceADeletedProjectVolcanoProjectIDNames(t *testing.T) {
	setConnectTestHome(t)
	t.Setenv("VOLCANO_PROJECT_ID", connectProjectID)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := t.TempDir()
	_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: connectProjectID, Name: "shop"})
	require.NoError(t, err)

	_, err = runConnect(t, server, dir)
	require.ErrorContains(t, err, "VOLCANO_PROJECT_ID is set to "+connectProjectID)
	assert.Zero(t, fake.creates)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectProjectID, link.Project.ID, "the link is unchanged")
}

func TestConnectWithARefusedBootstrapTokenGoesToBrowserLogin(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t, exchangeStatus: http.StatusUnauthorized, exchangeCode: api.BootstrapTokenUsed}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	out, err := runConnectHere(t, server, "--token", "vbt_one-time")
	// The fake refuses device authorization, so browser login fails; what
	// matters is that connect went there instead of to the saved login.
	require.ErrorContains(t, err, "browser authentication failed")
	assert.Contains(t, out, "The one-time token was already used. Signing in in the browser instead.")
	assert.True(t, fake.deviceAuthorize, "browser login started")
	assert.Zero(t, fake.creates, "the saved login, possibly another account, is never used")
}

func TestProjectNameAsksOnlyWhenNothingMarksTheRunNonInteractive(t *testing.T) {
	ask := func() string {
		var log bytes.Buffer
		name, err := projectName(options{in: strings.NewReader("typed\n"), log: &log}, "fallback")
		require.NoError(t, err)
		return name
	}
	t.Setenv("CI", "")
	t.Setenv("VOLCANO_NONINTERACTIVE", "")
	t.Setenv("TERM", "xterm-256color")
	assert.Equal(t, "typed", ask())

	for key, value := range map[string]string{"CI": "true", "VOLCANO_NONINTERACTIVE": "1", "TERM": "dumb"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			assert.Equal(t, "fallback", ask(), "an agent in a terminal is never asked")
		})
	}
}

func TestGenerateProjectName(t *testing.T) {
	assert.Regexp(t, `^[a-z]+-[a-z]+-\d{2}$`, generateProjectName())
}

func TestDisplayDir(t *testing.T) {
	assert.Equal(t, ".", displayDir("/w", "/w"))
	assert.Equal(t, "./app", displayDir("/w", "/w/app"))
	assert.Equal(t, "/other", displayDir("/w", "/other"))
	assert.Equal(t, "./..x", displayDir("/w", "/w/..x"))
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

// runConnectHere runs connect with no directory argument.
func runConnectHere(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(append([]string{"--skip-skills"}, args...))
	err := cmd.Execute()
	return stdout.String(), err
}

// chdirTemp moves into a new temp directory and returns its path as Getwd
// reports it, which on macOS resolves the /var symlink.
func chdirTemp(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	wd, err := os.Getwd()
	require.NoError(t, err)
	return wd
}

func useGeneratedName(t *testing.T, name string) {
	t.Helper()
	previous := generateName
	generateName = func() string { return name }
	t.Cleanup(func() { generateName = previous })
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
