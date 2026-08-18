package git

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	gitProjectID     = "33333333-3333-4333-8333-333333333333"
	gitConnectionID  = "77777777-7777-4777-8777-777777777777"
	gitInstallation  = int64(4242)
	gitRepositoryID  = int64(90210)
	connectionPath   = "/projects/" + gitProjectID + "/git-connection"
	deploySettings   = "/projects/" + gitProjectID + "/git-deploy-settings"
	connectionsPath  = "/user/git/connections"
	installationsURL = connectionsPath + "/" + gitConnectionID + "/installations"
	repositoriesURL  = installationsURL + "/4242/repositories"

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
	mu       sync.Mutex
	stdout   string
	commands []string
}

func (r *gitRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	return []byte(r.stdout), nil
}

func (r *gitRunner) ran() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...)
}

func executeGitCommand(t *testing.T, server *httptest.Server, runner *gitRunner, stdin string, args ...string) (string, error) {
	t.Helper()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
	if runner != nil {
		deps.GitCommandRunner = runner
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

// gitAPI is a fake platform: a project binding that starts as connected or not,
// one GitHub connection, one installation, and one visible repository.
type gitAPI struct {
	t *testing.T
	// connected is the repo the project is bound to, empty for none.
	connected string
	// connections, installations and repositories answer the resolve walk.
	connections   []map[string]any
	installations []map[string]any
	repositories  []map[string]any
	// autoDeploy reports what a push deploys once connected.
	autoDeploy bool
	// status overrides the response for every git route when non-zero.
	status int

	mu          sync.Mutex
	connectBody map[string]any
	deleted     bool
}

func newGitAPI(t *testing.T) *gitAPI {
	t.Helper()
	return &gitAPI{
		t:           t,
		connections: []map[string]any{githubConnection()},
		installations: []map[string]any{
			installation("octo", "User", "all"),
		},
		repositories: []map[string]any{repository("octo/storefront")},
		autoDeploy:   true,
	}
}

func (a *gitAPI) serve() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(a.handle))
	a.t.Cleanup(server.Close)
	return server
}

func (a *gitAPI) handle(w http.ResponseWriter, r *http.Request) {
	if a.status != 0 {
		writeGitJSON(a.t, w, a.status, map[string]any{"error": "git provider integration is not configured"})
		return
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == connectionsPath:
		writeGitJSON(a.t, w, http.StatusOK, map[string]any{"connections": a.connections})
	case r.Method == http.MethodGet && r.URL.Path == installationsURL:
		writeGitJSON(a.t, w, http.StatusOK, map[string]any{"installations": a.installations})
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/repositories"):
		writeGitJSON(a.t, w, http.StatusOK, map[string]any{"repositories": a.repositories})
	case r.Method == http.MethodGet && r.URL.Path == connectionPath:
		a.serveConnection(w)
	case r.Method == http.MethodPut && r.URL.Path == connectionPath:
		a.serveConnect(w, r)
	case r.Method == http.MethodDelete && r.URL.Path == connectionPath:
		a.serveDisconnect(w)
	case r.Method == http.MethodGet && r.URL.Path == deploySettings:
		writeGitJSON(a.t, w, http.StatusOK, map[string]any{
			"auto_deploy_enabled": a.autoDeploy,
			"deploy_functions":    a.autoDeploy,
			"updated_at":          "2026-08-18T00:00:00Z",
		})
	default:
		http.NotFound(w, r)
	}
}

func (a *gitAPI) serveConnection(w http.ResponseWriter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.connected == "" {
		writeGitJSON(a.t, w, http.StatusNotFound, map[string]any{"error": "project has no repo connection"})
		return
	}
	writeGitJSON(a.t, w, http.StatusOK, connectionPayload(a.connected))
}

func (a *gitAPI) serveConnect(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	require.NoError(a.t, json.NewDecoder(r.Body).Decode(&a.connectBody))

	name, _ := a.connectBody["repo_full_name"].(string)
	if name == "" {
		// The preferred selector is the numeric id, so resolve it the way the
		// platform would rather than echoing whatever was sent.
		name = "octo/storefront"
	}
	a.connected = name
	writeGitJSON(a.t, w, http.StatusOK, connectionPayload(name))
}

func (a *gitAPI) serveDisconnect(w http.ResponseWriter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.deleted = true
	a.connected = ""
	w.WriteHeader(http.StatusNoContent)
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

func connectionPayload(fullName string) map[string]any {
	return map[string]any{
		"repo_installation_id": gitInstallation,
		"repo_id":              gitRepositoryID,
		"repo_full_name":       fullName,
		"root_directory":       "",
		"production_branch":    "main",
		"updated_at":           "2026-08-18T00:00:00Z",
	}
}

func githubConnection() map[string]any {
	return map[string]any{
		"id":                    gitConnectionID,
		"provider":              "github",
		"provider_user_id":      "1",
		"provider_login":        "octo",
		"status":                "active",
		"last_authenticated_at": "2026-08-18T00:00:00Z",
		"created_at":            "2026-08-18T00:00:00Z",
		"updated_at":            "2026-08-18T00:00:00Z",
	}
}

func installation(login, accountType, selection string) map[string]any {
	return map[string]any{
		"id":                   gitInstallation,
		"account_login":        login,
		"account_type":         accountType,
		"repository_selection": selection,
	}
}

func repository(fullName string) map[string]any {
	return map[string]any{
		"id":             gitRepositoryID,
		"full_name":      fullName,
		"default_branch": "main",
		"private":        true,
	}
}

func writeGitJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
