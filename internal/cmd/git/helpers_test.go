package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	gitProjectID    = "33333333-3333-4333-8333-333333333333"
	gitConnectionID = "77777777-7777-4777-8777-777777777777"
	otherConnection = "88888888-8888-4888-8888-888888888888"
	gitInstallation = int64(4242)
	otherInstall    = int64(4343)
	gitRepositoryID = int64(90210)
	otherRepoID     = int64(90211)
	connectionPath  = "/projects/" + gitProjectID + "/git-connection"
	createRepoPath  = connectionPath + "/repository"
	deploySettings  = "/projects/" + gitProjectID + "/git-deploy-settings"
	connectionsPath = "/user/git/connections"

	originRemoteOutput = "origin\tgit@github.com:octo/storefront.git (fetch)\n" +
		"origin\tgit@github.com:octo/storefront.git (push)\n"
)

func setGitCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_WEB_URL", "https://volcano.test")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	cfg := &cliconfig.Config{
		UserToken:      "token",
		CurrentProject: &cliconfig.ProjectConfig{ID: gitProjectID, Name: "Storefront"},
	}
	require.NoError(t, cfg.Save())
}

// gitRunner records every git invocation so tests can assert the CLI only ever
// reads the local repository — no remote is written, and no credential is
// stored in .git/config.
type gitRunner struct {
	mu sync.Mutex
	// stdout answers `git remote -v`.
	stdout string
	// outputs answers any other command by its full command line. A command with
	// no entry fails, which is what git does for an unset config key — the
	// commands behind PushRemote read that as "the configuration does not say".
	outputs map[string]string
	// failConfig makes a config read fail the way a malformed config does,
	// which is not the same as a key being unset.
	failConfig bool
	commands   []string
}

func (r *gitRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	command := strings.TrimSpace(name + " " + strings.Join(args, " "))
	r.commands = append(r.commands, command)
	switch command {
	case "git remote -v":
		return []byte(r.stdout), nil
	case "git branch --show-current":
		// Detached HEAD, unless a test says otherwise: prints nothing, exits 0.
		return []byte(r.outputs[command]), nil
	default:
		if out, ok := r.outputs[command]; ok {
			return []byte(out), nil
		}
		if r.failConfig {
			return nil, exitStatus(128)
		}
		// An unset config key, which git signals with exit status 1 and nothing
		// else — the code tells that apart from a real failure.
		return nil, exitStatus(1)
	}
}

// exitStatus builds a real *exec.ExitError carrying code, by running a command
// that exits with it. ProcessState cannot be constructed by hand.
func exitStatus(code int) error {
	return exec.Command("sh", "-c", "exit "+strconv.Itoa(code)).Run()
}

func (r *gitRunner) ran() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

// allow makes one command succeed with no output. Writes are registered by their
// exact command line so a test asserts the arguments git is actually given,
// rather than that something vaguely like a remote was added.
func (r *gitRunner) allow(command string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outputs[command] = ""
}

// checkoutRunner answers the reads a create makes in a working checkout: a work
// tree, on branch, with a commit to push. remotes is what `git remote -v`
// reports, empty for a checkout with none.
func checkoutRunner(branch, remotes string) *gitRunner {
	return &gitRunner{
		stdout: remotes,
		outputs: map[string]string{
			"git rev-parse --is-inside-work-tree": "true\n",
			"git rev-parse --quiet --verify HEAD": "d34db33f\n",
			"git branch --show-current":           branch,
			// git is asked whether the remote name is one it accepts; an
			// unregistered name answers exit 1, which is git's "no".
			"git check-ref-format --allow-onelevel origin": "",
		},
	}
}

// allowRemoteName makes git accept name as a remote name.
func (r *gitRunner) allowRemoteName(name string) {
	r.allow("git check-ref-format --allow-onelevel " + name)
}

// gitTerminalRunner stands in for the git commands that get the user's terminal.
// It records them, so a test can assert that a push happened — or that it did
// not, which is the more important of the two.
type gitTerminalRunner struct {
	mu       sync.Mutex
	commands []string
	// output is what git writes while running, which the command passes straight
	// through to the user.
	output string
	err    error
}

func (r *gitTerminalRunner) Run(_ context.Context, out io.Writer, name string, args ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	if r.output != "" {
		fmt.Fprint(out, r.output)
	}
	return r.err
}

func (r *gitTerminalRunner) ran() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

func executeGitCommand(t *testing.T, server *httptest.Server, runner *gitRunner, stdin string, args ...string) (string, error) {
	t.Helper()
	return executeGitCommandWith(t, server, runner, nil, stdin, args...)
}

func executeGitCommandWith(
	t *testing.T,
	server *httptest.Server,
	runner *gitRunner,
	terminal cliruntime.TerminalCommandRunner,
	stdin string,
	args ...string,
) (string, error) {
	t.Helper()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
	if runner != nil {
		deps.GitCommandRunner = runner
	}
	if terminal != nil {
		deps.GitTerminalRunner = terminal
	}

	cmd := New(deps)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(bytes.NewBufferString(stdin))
	cmd.SetArgs(args)

	err := cmd.Execute()
	return out.String(), err
}

// gitAPI is a fake platform. Installations are keyed by connection and repos by
// installation, so a test can lay out a resolve walk that actually has to keep
// looking rather than finding the answer on its first try.
type gitAPI struct {
	t *testing.T
	// connected is the repo the project is bound to, empty for none.
	connected string
	// connectedRoot is that binding's root directory.
	connectedRoot string
	// connectedRepoID overrides the bound repository's id. Left zero it is
	// derived from the name, so a binding to another repository really is
	// another repository — the id is what decides a replacement, not the name.
	connectedRepoID int64
	// connectedInstallation overrides the binding's installation id, which a
	// reinstall of the App changes.
	connectedInstallation int64
	// connectedBranch overrides the binding's cached production branch, which
	// goes stale when the repository's GitHub default branch moves.
	connectedBranch string
	// connectedAfterRead makes the binding change on the next read, modelling
	// something else repointing the project while a prompt is open.
	connectedAfterRead string
	// disconnectAfterRead is the same for a binding that goes away.
	disconnectAfterRead bool
	// projectMissing makes GET /projects/{id} answer 404, which is what a
	// deleted project or a VOLCANO_PROJECT_ID naming nothing looks like.
	projectMissing bool
	// projectReadStatus fails only GET /projects/{id}, leaving the binding read
	// to answer its own 404 — the shape where the project can be neither
	// confirmed nor ruled out.
	projectReadStatus int
	// rootAfterRead changes only the binding's root directory on the next read,
	// leaving the repository alone.
	rootAfterRead string
	// connectionUpdated overrides the binding's updated_at, which the platform
	// bumps whenever the row really changes.
	connectionUpdated string

	connections               []map[string]any
	installationsByConnection map[string][]map[string]any
	reposByInstallation       map[int64][]map[string]any
	// installationsStatus overrides the installations response per connection,
	// so a test can make one connection fail while another still answers.
	installationsStatus map[string]int
	// repositoriesStatus overrides the repositories response per installation.
	repositoriesStatus map[int64]int

	autoDeploy      bool
	deployFunctions bool
	frontend        string
	frontendRoot    string
	// providerStatus overrides the response on the routes that reach the git
	// provider. The project's own binding routes are excluded on purpose: they
	// only read the database, so the real server cannot answer 503 there.
	providerStatus int
	// projectStatus overrides the response on the project binding routes.
	projectStatus int
	// deploySettingsStatus fails only the deploy-settings read, which the
	// commands tolerate without failing the connect.
	deploySettingsStatus int

	// createStatus overrides the create-repository response, and
	// createErrorMessage the message it carries — the platform's message is the
	// only thing that tells the causes of a shared status apart.
	createStatus       int
	createErrorMessage string
	// createAppInstalled reports whether the App installation covers the new
	// repository. False is not a failure: the repository and the binding both
	// exist, and no push deploys.
	createAppInstalled bool
	createInstallURL   string
	// createHangsUp drops the connection after the request arrives, without
	// answering. The repository may exist and nothing said so, which is the worst
	// of the ambiguous outcomes and carries no HTTP status to classify.
	createHangsUp bool

	mu              sync.Mutex
	connectBody     map[string]any
	createBody      map[string]any
	createCalls     int
	deleted         bool
	connectionReads int
}

func newGitAPI(t *testing.T) *gitAPI {
	t.Helper()
	return &gitAPI{
		t:           t,
		connections: []map[string]any{githubConnection(gitConnectionID, "octo")},
		installationsByConnection: map[string][]map[string]any{
			gitConnectionID: {installation(gitInstallation, "octo", "User", "all")},
		},
		reposByInstallation: map[int64][]map[string]any{
			gitInstallation: {repository("octo/storefront")},
		},
		installationsStatus: map[string]int{},
		repositoriesStatus:  map[int64]int{},
		autoDeploy:          true,
		deployFunctions:     true,
		// Connecting a repository turns auto-deploy on, and a create is a connect,
		// so the App covering the new repository is the ordinary case.
		createAppInstalled: true,
	}
}

func (a *gitAPI) serve() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(a.handle))
	a.t.Cleanup(server.Close)
	return server
}

func (a *gitAPI) handle(w http.ResponseWriter, r *http.Request) {
	if a.providerStatus != 0 && strings.HasPrefix(r.URL.Path, "/user/git/") {
		writeGitJSON(a.t, w, a.providerStatus, map[string]any{"error": "git provider integration is not configured"})
		return
	}
	if a.projectStatus != 0 && strings.HasPrefix(r.URL.Path, "/projects/") {
		writeGitJSON(a.t, w, a.projectStatus, map[string]any{"error": "upstream unavailable"})
		return
	}

	switch {
	case r.Method == http.MethodGet && isProjectPath(r.URL.Path):
		a.serveProject(w, r)
	case r.Method == http.MethodGet && r.URL.Path == connectionsPath:
		writeGitJSON(a.t, w, http.StatusOK, map[string]any{"connections": a.connections})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/installations"):
		a.serveInstallations(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/repositories"):
		a.serveRepositories(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git-connection/repository"):
		a.serveCreateRepository(w, r)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git-connection"):
		a.serveConnection(w)
	case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/git-connection"):
		a.serveConnect(w, r)
	case r.Method == http.MethodDelete && strings.HasSuffix(r.URL.Path, "/git-connection"):
		a.serveDisconnect(w)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/git-deploy-settings"):
		a.serveDeploySettings(w)
	default:
		http.NotFound(w, r)
	}
}

// connectionOf reads the connection id out of
// /user/git/connections/{id}/installations[/{installationId}/repositories].
func connectionOf(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 4 {
		return ""
	}
	return segments[3]
}

// installationOf reads the installation id out of
// /user/git/connections/{id}/installations/{installationId}/repositories.
func installationOf(t *testing.T, path string) int64 {
	t.Helper()
	segments := strings.Split(strings.Trim(path, "/"), "/")
	require.Len(t, segments, 7, "unexpected repositories path %q", path)
	id, err := strconv.ParseInt(segments[5], 10, 64)
	require.NoError(t, err)
	return id
}

func (a *gitAPI) serveInstallations(w http.ResponseWriter, r *http.Request) {
	connection := connectionOf(r.URL.Path)
	if status := a.installationsStatus[connection]; status != 0 {
		writeGitJSON(a.t, w, status, map[string]any{"error": "github rejected the token for " + connection})
		return
	}
	writeGitJSON(a.t, w, http.StatusOK, map[string]any{
		"installations": a.installationsByConnection[connection],
	})
}

func (a *gitAPI) serveRepositories(w http.ResponseWriter, r *http.Request) {
	id := installationOf(a.t, r.URL.Path)
	if status := a.repositoriesStatus[id]; status != 0 {
		writeGitJSON(a.t, w, status, map[string]any{"error": "github rejected the request"})
		return
	}
	writeGitJSON(a.t, w, http.StatusOK, map[string]any{
		"repositories": a.reposByInstallation[id],
	})
}

func (a *gitAPI) currentInstallation() int64 {
	if a.connectedInstallation != 0 {
		return a.connectedInstallation
	}
	return gitInstallation
}

// isProjectPath matches "/projects/{id}" and nothing below it, so any project
// id a test selects is served.
func isProjectPath(path string) bool {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	return len(segments) == 2 && segments[0] == "projects"
}

// serveProject answers the read that tells a project with no binding apart from
// a project that does not exist.
func (a *gitAPI) serveProject(w http.ResponseWriter, r *http.Request) {
	if a.projectReadStatus != 0 {
		writeGitJSON(a.t, w, a.projectReadStatus, map[string]any{"error": "project read failed"})
		return
	}
	if a.projectMissing {
		writeGitJSON(a.t, w, http.StatusNotFound, map[string]any{"error": "project not found"})
		return
	}
	segments := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	writeGitJSON(a.t, w, http.StatusOK, map[string]any{
		"id":               segments[1],
		"name":             "Storefront",
		"status":           "active",
		"all_regions":      false,
		"selected_regions": []string{"aws-us-east-1"},
		"created_at":       "2026-08-18T00:00:00Z",
	})
}

func (a *gitAPI) serveConnection(w http.ResponseWriter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	// The first read is what the prompt shows; changing the binding only from
	// the second read on models something else repointing the project while
	// that prompt was open.
	a.connectionReads++
	if a.connectionReads > 1 {
		switch {
		case a.connectedAfterRead != "":
			a.connected, a.connectedAfterRead = a.connectedAfterRead, ""
			a.connectedRepoID = otherRepoID
		case a.disconnectAfterRead:
			a.connected, a.disconnectAfterRead = "", false
		case a.rootAfterRead != "":
			a.connectedRoot, a.rootAfterRead = a.rootAfterRead, ""
			a.connectionUpdated = "2026-08-18T00:00:01Z"
		}
	}
	if a.connected == "" {
		writeGitJSON(a.t, w, http.StatusNotFound, map[string]any{"error": "project has no repo connection"})
		return
	}
	payload := connectionPayload(a.connected, a.connectedRoot, a.currentRepoID(), a.currentInstallation())
	if a.connectedBranch != "" {
		payload["production_branch"] = a.connectedBranch
	}
	if a.connectionUpdated != "" {
		payload["updated_at"] = a.connectionUpdated
	}
	writeGitJSON(a.t, w, http.StatusOK, payload)
}

// currentRepoID derives the bound repository's id from its name unless a test
// pinned one: a different name means a different repository, except where a
// test is deliberately modelling a rename.
func (a *gitAPI) currentRepoID() int64 {
	switch {
	case a.connectedRepoID != 0:
		return a.connectedRepoID
	case strings.EqualFold(a.connected, "octo/storefront"):
		return gitRepositoryID
	default:
		return otherRepoID
	}
}

func (a *gitAPI) serveConnect(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	require.NoError(a.t, json.NewDecoder(r.Body).Decode(&a.connectBody))

	// The preferred selector is the numeric id, so resolve it the way the
	// platform would rather than echoing a fixed name: a fake that answers
	// "octo/storefront" whatever it is sent cannot fail a test that binds the
	// wrong repository.
	selector, ok := a.connectBody["repository_id"].(float64)
	require.True(a.t, ok, "the bind must select by repository id")
	full := a.repositoryByID(int64(selector))
	require.NotEmpty(a.t, full, "repository_id %v is not in this fixture", selector)

	a.connected = full
	a.connectedRepoID = repoIDFor(full)
	a.connectedRoot, _ = a.connectBody["root_directory"].(string)
	if id, ok := a.connectBody["installation_id"].(float64); ok {
		a.connectedInstallation = int64(id)
	}
	writeGitJSON(a.t, w, http.StatusOK,
		connectionPayload(a.connected, a.connectedRoot, a.connectedRepoID, a.currentInstallation()))
}

// serveCreateRepository models the create: the repository is named by the
// request, so a fake that answered with a fixed name could not fail a test that
// created the wrong one.
func (a *gitAPI) serveCreateRepository(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	require.NoError(a.t, json.NewDecoder(r.Body).Decode(&a.createBody))
	a.createCalls++

	if a.createHangsUp {
		// The request was received in full; only the answer is lost. Hijacking and
		// closing is the one way to produce that from a test server.
		hijacker, ok := w.(http.Hijacker)
		require.True(a.t, ok, "the test server must support hijacking")
		conn, _, err := hijacker.Hijack()
		require.NoError(a.t, err)
		require.NoError(a.t, conn.Close())
		return
	}
	if a.createStatus != 0 {
		message := a.createErrorMessage
		if message == "" {
			message = "the repository could not be created"
		}
		writeGitJSON(a.t, w, a.createStatus, map[string]any{"error": message})
		return
	}

	name, ok := a.createBody["name"].(string)
	require.True(a.t, ok, "the create must name the repository")
	owner, hasOwner := a.createBody["owner"].(string)
	if !hasOwner {
		// The connected account, which the platform resolves when owner is
		// omitted. The CLI must not be sending a login of its own here.
		owner = "octo"
	}
	full := owner + "/" + name
	root, _ := a.createBody["root_directory"].(string)
	branch, named := a.createBody["production_branch"].(string)
	if !named {
		// What the platform falls back to for a repository created empty: the
		// account's configured default branch name, which is a prediction.
		branch = "main"
	}

	a.connected, a.connectedRepoID = full, repoIDFor(full)
	a.connectedRoot, a.connectedBranch = root, branch
	payload := connectionPayload(full, root, a.connectedRepoID, a.currentInstallation())
	payload["production_branch"] = branch
	created := map[string]any{"connection": payload, "app_installed": a.createAppInstalled}
	if !a.createAppInstalled && a.createInstallURL != "" {
		created["install_url"] = a.createInstallURL
	}
	writeGitJSON(a.t, w, http.StatusCreated, created)
}

func (a *gitAPI) sentCreateBody() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.createBody
}

func (a *gitAPI) createAttempts() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.createCalls
}

func (a *gitAPI) serveDisconnect(w http.ResponseWriter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleted = true
	a.connected = ""
	w.WriteHeader(http.StatusNoContent)
}

func (a *gitAPI) serveDeploySettings(w http.ResponseWriter) {
	if a.deploySettingsStatus != 0 {
		writeGitJSON(a.t, w, a.deploySettingsStatus, map[string]any{"error": "settings unavailable"})
		return
	}
	settings := map[string]any{
		"auto_deploy_enabled": a.autoDeploy,
		"deploy_functions":    a.deployFunctions,
		"updated_at":          "2026-08-18T00:00:00Z",
	}
	if a.frontend != "" {
		settings["frontend_name"] = a.frontend
	}
	if a.frontendRoot != "" {
		settings["frontend_app_root"] = a.frontendRoot
	}
	writeGitJSON(a.t, w, http.StatusOK, settings)
}

func (a *gitAPI) sentConnectBody() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connectBody
}

func (a *gitAPI) disconnectCalled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.deleted
}

func connectionPayload(fullName, rootDirectory string, repoID, installationID int64) map[string]any {
	return map[string]any{
		"repo_installation_id": installationID,
		"repo_id":              repoID,
		"repo_full_name":       fullName,
		"root_directory":       rootDirectory,
		"production_branch":    "main",
		"updated_at":           "2026-08-18T00:00:00Z",
	}
}

func githubConnection(id, login string) map[string]any {
	return map[string]any{
		"id":                    id,
		"provider":              "github",
		"provider_user_id":      "1",
		"provider_login":        login,
		"status":                "active",
		"last_authenticated_at": "2026-08-18T00:00:00Z",
		"created_at":            "2026-08-18T00:00:00Z",
		"updated_at":            "2026-08-18T00:00:00Z",
	}
}

func installation(id int64, login, accountType, selection string) map[string]any {
	return map[string]any{
		"id":                   id,
		"account_login":        login,
		"account_type":         accountType,
		"repository_selection": selection,
	}
}

func repository(fullName string) map[string]any {
	return map[string]any{
		"id":             repoIDFor(fullName),
		"full_name":      fullName,
		"default_branch": "main",
		"private":        true,
	}
}

// repoIDFor gives every repository in a fixture a distinct id.
//
// One shared id let a test assert "Connected octo/storefront" while the command
// had sent the id of a different repository, because serveConnect echoed a fixed
// name back regardless of the selector. The wrong-repository tests then passed
// only because the decoy was absent from the fixture, so resolving it failed —
// which is not the same as asserting the right one was chosen, and it is the
// failure class this whole feature is about.
func repoIDFor(fullName string) int64 {
	if strings.EqualFold(fullName, "octo/storefront") {
		return gitRepositoryID // the primary, so existing assertions still name it
	}
	id := int64(1)
	for _, b := range []byte(strings.ToLower(fullName)) {
		id = id*31 + int64(b)
	}
	return gitRepositoryID + 1 + id%100000
}

// repositoryByID reverses the selector the way the platform does, so the fake
// reports the repository it was actually asked to bind.
func (a *gitAPI) repositoryByID(id int64) string {
	for _, repositories := range a.reposByInstallation {
		for _, candidate := range repositories {
			if full, ok := candidate["full_name"].(string); ok && repoIDFor(full) == id {
				return full
			}
		}
	}
	return ""
}

func writeGitJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
