package auth

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	authProjectID = "11111111-1111-4111-8111-111111111111"
	// Fake credentials: these tests only check that login forwards, rejects, or
	// saves the token it is given.
	authTestAccountToken  = "valid-token"
	authTestRejectedToken = "bad-token"
	authTestProjectToken  = cliconfig.ProjectTokenPrefix + "project-token"
	// signupBrowserDeps' API server is a loopback address, so cfg.WebURLForAPIURL derives
	// http://localhost:3000 with no VOLCANO_WEB_URL set; next is always a same-origin
	// /device path (not derived from the device-flow verification URI -- see Signup's
	// doc comment in internal/auth/auth.go).
	authTestSignupURL = "http://localhost:3000/signup?email=ted%40example.com&next=%2Fdevice%3Fuser_code%3DABCD-EFGH&source=cli"
	// Used when VOLCANO_WEB_URL is explicitly set to something the API URL wouldn't derive.
	authTestSignupOverrideURL = "https://custom.example/signup?email=ted%40example.com&next=%2Fdevice%3Fuser_code%3DABCD-EFGH&source=cli"
)

func TestLoginTokenSuccessSavesConfig(t *testing.T) {
	setAuthTestHome(t)
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects" {
			http.NotFound(w, r)
			return
		}
		sawAuth = r.Header.Get("Authorization")
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				map[string]any{
					"id": authProjectID,
				},
			},
			"has_more": false,
			"page":     1,
			"limit":    100,
			"total":    0,
		})
	}))
	defer server.Close()

	out, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "--token", authTestAccountToken)
	require.NoError(t, err)
	assert.Equal(t, "Bearer "+authTestAccountToken, sawAuth)
	assert.Contains(t, out, "Token validated")
	assert.Contains(t, out, "Logged in successfully")

	cfg := loadAuthTestConfig(t)
	assert.Equal(t, authTestAccountToken, cfg.UserToken)
	assert.Empty(t, cfg.UserID)
}

func TestLoginTokenInvalidFailsWithoutSavingConfig(t *testing.T) {
	setAuthTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}))
	defer server.Close()

	_, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "--token", authTestRejectedToken)
	require.ErrorContains(t, err, "token authentication failed: invalid token")

	path, err := cliconfig.Path()
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "config should not be saved after invalid token, stat err: %v", err)
}

func TestLoginProjectTokenSelectsTheProject(t *testing.T) {
	setAuthTestHome(t)
	var sawPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"id":         authProjectID,
			"name":       "Alpha",
			"status":     "active",
			"created_at": "2026-05-20T00:00:00Z",
			"updated_at": "2026-05-20T00:00:00Z",
		})
	}))
	defer server.Close()

	out, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"--token", authTestProjectToken, "--project", authProjectID)
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+authProjectID, sawPath)
	assert.Contains(t, out, "Token validated")
	assert.Contains(t, out, "Now using project: Alpha ("+authProjectID+")")

	cfg := loadAuthTestConfig(t)
	assert.Equal(t, authTestProjectToken, cfg.UserToken)
	require.NotNil(t, cfg.CurrentProject)
	assert.Equal(t, authProjectID, cfg.CurrentProject.ID)
	assert.Equal(t, "Alpha", cfg.CurrentProject.Name)
}

// authDeviceCode is the device code the dashboard put in the prompt. It is
// distinctive so a test can check it never shows up anywhere.
const authDeviceCode = "dc-dashboard-secret-7f3a"

// serveDeviceLogin answers the device token poll with an approved token and
// the platform exchange after it, and reports whether it answered r.
func serveDeviceLogin(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	switch r.URL.Path {
	case "/auth/device/token":
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, authDeviceCode, body["device_code"])
		writeAuthJSON(t, w, http.StatusOK, map[string]any{"access_token": "auth-access-token", "token_type": "bearer", "expires_in": 3600})
	case "/auth/platform/exchange":
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"token": "pk-exchanged", "user_id": "user-1", "token_id": "33333333-3333-4333-8333-333333333333",
			"expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	default:
		return false
	}
	return true
}

func TestLoginWithADeviceCodeKeepsTheLoginWhenTheProjectIsWrong(t *testing.T) {
	setAuthTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveDeviceLogin(t, w, r) {
			return
		}
		writeAuthJSON(t, w, http.StatusNotFound, map[string]string{"error": "project not found"})
	}))
	defer server.Close()

	out, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"--device-code", authDeviceCode, "--project", authProjectID)
	require.ErrorContains(t, err, "device code authentication failed: token is not valid for project "+authProjectID)
	assert.Equal(t, http.StatusNotFound, api.Status(err), "the exit code still reports the missing project")
	assert.Contains(t, out, "The code from the dashboard is spent, but its login is saved, so you are logged in.")
	assert.Contains(t, out, "volcano use <project>")
	assert.NotContains(t, out, authDeviceCode)

	cfg := loadAuthTestConfig(t)
	assert.Equal(t, "pk-exchanged", cfg.UserToken)
	assert.Nil(t, cfg.CurrentProject)
}

func TestLoginWithADeviceCodeSignsInWithoutTheBrowser(t *testing.T) {
	setAuthTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveDeviceLogin(t, w, r) {
			return
		}
		require.Equal(t, "/projects", r.URL.Path, "no device authorization: the dashboard did that")
		writeAuthJSON(t, w, http.StatusOK, map[string]any{"data": []any{}, "has_more": false, "page": 1, "limit": 1, "total": 0})
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL, OpenBrowser: func(string) error {
		t.Error("a device code from the dashboard never opens the browser")
		return nil
	}}

	stdout, stderr, err := executeLoginWithDeps(t, deps, "--device-code", authDeviceCode)
	require.NoError(t, err)
	assert.Contains(t, stdout, "Signed in with the code from the dashboard")
	assert.Contains(t, stdout, "Logged in successfully")
	assert.NotContains(t, stdout+stderr, authDeviceCode)

	cfg := loadAuthTestConfig(t)
	assert.Equal(t, "pk-exchanged", cfg.UserToken)
	assert.Equal(t, "user-1", cfg.UserID)
	path, err := cliconfig.Path()
	require.NoError(t, err)
	assert.NotContains(t, readAuthFile(t, path), authDeviceCode)
}

// login does not fall back to the browser for a refused code: it fails, as
// not signed in, so a script can tell.
func TestLoginWithARefusedDeviceCodeFailsAsNotSignedIn(t *testing.T) {
	setAuthTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/auth/device/token", r.URL.Path, "no browser login")
		writeAuthJSON(t, w, http.StatusBadRequest, map[string]string{"error": "expired_token", "error_description": "device code expired"})
	}))
	defer server.Close()

	stdout, stderr, err := executeLogin(t, server, "--device-code", authDeviceCode)
	require.EqualError(t, err, "device code authentication failed: the code from the dashboard has expired. "+
		"Copy a new prompt from the dashboard, or sign in in the browser")
	require.ErrorIs(t, err, cliconfig.ErrNotAuthenticated)
	assert.NotContains(t, stdout+stderr, authDeviceCode)
	path, err := cliconfig.Path()
	require.NoError(t, err)
	assert.NoFileExists(t, path, "nothing is saved")
}

func TestLoginRefusesATokenAndADeviceCodeTogether(t *testing.T) {
	setAuthTestHome(t)

	_, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{}), "--token", "pk-account", "--device-code", authDeviceCode)
	require.ErrorContains(t, err, "none of the others can be")
}

// Nothing can discover which project a pt- token belongs to: listing projects
// is exactly what it cannot do. Saying so beats saving a credential that then
// fails on every command.
func TestLoginProjectTokenWithoutAProjectFails(t *testing.T) {
	setAuthTestHome(t)

	_, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{}), "--token", authTestProjectToken)
	require.ErrorContains(t, err, "--project <project-id>")

	path, err := cliconfig.Path()
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "config should not be saved, stat err: %v", err)
}

// `volcano use` takes a project name, so --project takes one too rather than
// failing on the uuid parser.
func TestLoginAccountTokenSelectsTheProjectByName(t *testing.T) {
	setAuthTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/projects", r.URL.Path)
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{map[string]any{
				"id":         authProjectID,
				"name":       "my-app",
				"status":     "active",
				"created_at": "2026-05-20T00:00:00Z",
				"updated_at": "2026-05-20T00:00:00Z",
			}},
			"has_more": false,
			"page":     1,
			"limit":    100,
			"total":    1,
		})
	}))
	defer server.Close()

	out, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"--token", cliconfig.AccountTokenPrefix+"account-token", "--project", "my-app")
	require.NoError(t, err)
	assert.Contains(t, out, "Now using project: my-app ("+authProjectID+")")

	cfg := loadAuthTestConfig(t)
	require.NotNil(t, cfg.CurrentProject)
	assert.Equal(t, authProjectID, cfg.CurrentProject.ID)
}

const (
	authOtherProjectID = "22222222-2222-4222-8222-222222222222"
	authAlphaAnonKey   = "VOLCANO_ANON_KEY=ak-alpha\n"
)

// linkedToAlpha links a new directory to Alpha, gives it a cloud env file with
// Alpha's anon key, and runs the test from it. It returns the directory.
func linkedToAlpha(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: authProjectID, Name: "Alpha"})
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "volcano", "cloud.env"), []byte(authAlphaAnonKey), 0o600))
	t.Chdir(dir)
	return dir
}

// betaServer answers Beta's project read, its anon keys, and a device login.
func betaServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveDeviceLogin(t, w, r) {
			return
		}
		switch r.URL.Path {
		case "/projects/" + authOtherProjectID:
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"id": authOtherProjectID, "name": "Beta", "status": "active",
				"created_at": "2026-05-20T00:00:00Z", "updated_at": "2026-05-20T00:00:00Z",
			})
		case "/projects/" + authOtherProjectID + "/anon-keys":
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"data": []any{map[string]any{"id": "44444444-4444-4444-8444-444444444444", "name": "default", "key_value": "ak-beta", "is_default": true}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestLoginProjectInsideALinkedDirectoryUpdatesTheLink(t *testing.T) {
	for name, tc := range map[string]struct {
		login            []string
		savedToken, undo string
		// global is the project saved in ~/.volcano/config.json.
		global *cliconfig.ProjectConfig
	}{
		"account token": {login: []string{"--token", "pk-account"}, savedToken: "pk-account", undo: "volcano use " + authProjectID},
		"device code":   {login: []string{"--device-code", authDeviceCode}, savedToken: "pk-exchanged", undo: "volcano use " + authProjectID},
		// A project token reaches only its project, so the global selection
		// follows it, or commands outside the link would send it elsewhere.
		"project token": {
			login: []string{"--token", authTestProjectToken}, savedToken: authTestProjectToken,
			undo:   "volcano login --token <token for that project> --project " + authProjectID,
			global: &cliconfig.ProjectConfig{ID: authOtherProjectID, Name: "Beta"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			setAuthTestHome(t)
			dir := linkedToAlpha(t)
			server := betaServer(t)

			stdout, stderr, err := executeLogin(t, server, append(tc.login, "--project", authOtherProjectID)...)
			require.NoError(t, err)

			link, err := cliconfig.FindLink(dir)
			require.NoError(t, err)
			assert.Equal(t, cliconfig.ProjectConfig{ID: authOtherProjectID, Name: "Beta"}, link.Project)
			assert.Contains(t, stdout, "Now using project: Beta ("+authOtherProjectID+")")
			assert.Contains(t, stdout, "Project saved to "+link.Path())
			assert.Contains(t, stdout, "Updated the anon key in "+filepath.Join(dir, "volcano", "cloud.env"))
			assert.Equal(t, "Warning: This directory was linked to Alpha ("+authProjectID+"). It is now linked to Beta ("+
				authOtherProjectID+"). To undo, run: "+tc.undo+"\n", stderr)
			assert.Contains(t, readAuthFile(t, filepath.Join(dir, "volcano", "cloud.env")), "VOLCANO_ANON_KEY=ak-beta\n")

			cfg := loadAuthTestConfig(t)
			assert.Equal(t, tc.savedToken, cfg.UserToken)
			assert.Equal(t, tc.global, cfg.CurrentProject, "an account login leaves the global selection alone, as with volcano use")
		})
	}
}

func TestLoginProjectInsideADirectoryAlreadyLinkedToItDoesNotWarn(t *testing.T) {
	setAuthTestHome(t)
	dir := t.TempDir()
	_, err := cliconfig.WriteLink(dir, cliconfig.ProjectConfig{ID: authOtherProjectID, Name: "Beta"})
	require.NoError(t, err)
	t.Chdir(dir)
	server := betaServer(t)

	stdout, stderr, err := executeLogin(t, server, "--token", "pk-account", "--project", authOtherProjectID)
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "Project saved to "+filepath.Join(dir, ".volcano", "project.json"))
}

func TestLoginProjectOutsideALinkedDirectorySavesTheGlobalSelection(t *testing.T) {
	setAuthTestHome(t)
	t.Chdir(t.TempDir())
	server := betaServer(t)

	stdout, stderr, err := executeLogin(t, server, "--token", "pk-account", "--project", authOtherProjectID)
	require.NoError(t, err)
	assert.Empty(t, stderr)
	assert.Contains(t, stdout, "Project saved to ~/.volcano/config.json")

	cfg := loadAuthTestConfig(t)
	require.NotNil(t, cfg.CurrentProject)
	assert.Equal(t, authOtherProjectID, cfg.CurrentProject.ID)
}

// Inside a linked directory, VOLCANO_PROJECT_ID would keep overriding the new
// link, so login refuses as `volcano use` does.
func TestLoginProjectInsideALinkedDirectoryRefusesWhileVolcanoProjectIDNamesAnotherProject(t *testing.T) {
	for name, tc := range map[string]struct {
		login      []string
		savedToken string
	}{
		"account token": {login: []string{"--token", "pk-account"}},
		"device code":   {login: []string{"--device-code", authDeviceCode}, savedToken: "pk-exchanged"},
	} {
		t.Run(name, func(t *testing.T) {
			setAuthTestHome(t)
			dir := linkedToAlpha(t)
			t.Setenv("VOLCANO_PROJECT_ID", authProjectID)
			server := betaServer(t)

			stdout, _, err := executeLogin(t, server, append(tc.login, "--project", authOtherProjectID)...)
			require.ErrorContains(t, err, "VOLCANO_PROJECT_ID is set to "+authProjectID)

			link, err := cliconfig.FindLink(dir)
			require.NoError(t, err)
			assert.Equal(t, authProjectID, link.Project.ID, "the link is unchanged")
			assert.Equal(t, authAlphaAnonKey, readAuthFile(t, filepath.Join(dir, "volcano", "cloud.env")))
			cfg := loadAuthTestConfig(t)
			assert.Equal(t, tc.savedToken, cfg.UserToken, "only a spent device code's login is saved")
			assert.Nil(t, cfg.CurrentProject)
			if tc.savedToken != "" {
				assert.Contains(t, stdout, "The code from the dashboard is spent, but its login is saved")
			}
		})
	}
}

// writeBadLink gives a new directory a project link that can't be parsed, runs
// the test from it, and returns the link's path.
func writeBadLink(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".volcano", "project.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("<<<<<<< HEAD\n"), 0o644))
	t.Chdir(dir)
	return path
}

func TestLoginQuotesADirectoryWithASpaceInTheBadLinkWarning(t *testing.T) {
	setAuthTestHome(t)
	dir := filepath.Join(t.TempDir(), "My App")
	path := filepath.Join(dir, ".volcano", "project.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("<<<<<<< HEAD\n"), 0o644))
	t.Chdir(dir)
	server := betaServer(t)

	_, stderr, err := executeLogin(t, server, "--token", "pk-account", "--project", authOtherProjectID)
	require.NoError(t, err)
	wd, err := os.Getwd()
	require.NoError(t, err)
	assert.Contains(t, stderr, "volcano connect '"+wd+"' --project <project-id>")
}

func TestLoginWarnsAboutABadProjectLinkAndIgnoresIt(t *testing.T) {
	setAuthTestHome(t)
	path := writeBadLink(t)
	server := betaServer(t)

	stdout, stderr, err := executeLogin(t, server, "--token", "pk-account", "--project", authOtherProjectID)
	require.NoError(t, err)
	assert.Contains(t, stderr, "Warning: Ignoring a project link that can't be used: failed to parse project link "+path)
	assert.Contains(t, stderr, "volcano connect "+filepath.Dir(filepath.Dir(path))+" --project <project-id>")
	assert.Contains(t, stdout, "Project saved to ~/.volcano/config.json", "with the link ignored, the selection is global")

	cfg := loadAuthTestConfig(t)
	assert.Equal(t, "pk-account", cfg.UserToken)
	require.NotNil(t, cfg.CurrentProject)
	assert.Equal(t, authOtherProjectID, cfg.CurrentProject.ID)
	assert.Equal(t, "<<<<<<< HEAD\n", readAuthFile(t, path), "login leaves the link alone")
}

func TestLoginProjectFlagHelpNamesWhatItTakes(t *testing.T) {
	out, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{}), "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "Project ID or name")
	assert.Contains(t, out, "which must use the ID")
}

func TestLoginProjectFlagNeedsAToken(t *testing.T) {
	setAuthTestHome(t)

	_, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{}), "--project", authProjectID)
	require.ErrorContains(t, err, "--project applies to --token and --device-code login")
}

func TestLogoutDeletesConfigWithOnlyCredentials(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "token"})

	out, err := executeAuthCommand(t, NewLogout())
	require.NoError(t, err)
	assert.Contains(t, out, "Logged out")

	path, err := cliconfig.Path()
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "config exists after logout: %v", err)
}

func TestLogoutKeepsMachineSettings(t *testing.T) {
	setAuthTestHome(t)
	telemetry := &cliconfig.TelemetryConfig{DeviceID: "device", Disabled: true, NoticeShown: true}
	docs := &cliconfig.DocsSourceConfig{Repo: "Kong/volcano-docs"}
	saveAuthTestConfig(t, &cliconfig.Config{
		UserToken:       "token",
		UserID:          "user",
		CurrentProject:  &cliconfig.ProjectConfig{ID: "p1", Name: "app"},
		FunctionAliases: map[string]map[string]string{"scope": {"a": "f1"}},
		DocsSource:      docs,
		Telemetry:       telemetry,
	})

	_, err := executeAuthCommand(t, NewLogout())
	require.NoError(t, err)

	assert.Equal(t, &cliconfig.Config{DocsSource: docs, Telemetry: telemetry}, loadAuthTestConfig(t))
}

func TestSignupUsesGitEmailDefault(t *testing.T) {
	setAuthTestHome(t)
	deps, openedURL, pollTicker := signupBrowserDeps(t)
	deps.GitCommandRunner = cliruntime.CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		assert.Equal(t, "git", name)
		assert.Equal(t, []string{"config", "--global", "user.email"}, args)
		return []byte("ted@example.com\n"), nil
	})

	// No VOLCANO_WEB_URL set, so signup derives its origin from the API URL.
	out, err := executeAuthCommandWithInputAndTick(t, NewSignup(deps), "\n", pollTicker)
	require.NoError(t, err)
	assert.Equal(t, authTestSignupURL, *openedURL)
	assert.Contains(t, out, "[ted@example.com]")
	assert.Contains(t, out, "Opening browser: "+authTestSignupURL)
	assert.Contains(t, out, "Signed up and logged in successfully")

	cfg := loadAuthTestConfig(t)
	assert.Equal(t, "platform-token", cfg.UserToken)
	assert.Equal(t, "platform-user-1", cfg.UserID)
}

func TestSignupWarnsAboutABadProjectLinkAndIgnoresIt(t *testing.T) {
	setAuthTestHome(t)
	path := writeBadLink(t)
	deps, _, pollTicker := signupBrowserDeps(t)

	out, err := executeAuthCommandWithInputAndTick(t, NewSignup(deps), "ted@example.com\n", pollTicker)
	require.NoError(t, err)
	assert.Contains(t, out, "Warning: Ignoring a project link that can't be used: failed to parse project link "+path)
	assert.Contains(t, out, "Signed up and logged in successfully")
	assert.Equal(t, "platform-token", loadAuthTestConfig(t).UserToken)
}

func TestSignupAllowsEmailOverride(t *testing.T) {
	setAuthTestHome(t)
	deps, openedURL, pollTicker := signupBrowserDeps(t)
	deps.GitCommandRunner = cliruntime.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ted@example.com\n"), nil
	})

	_, err := executeAuthCommandWithInputAndTick(t, NewSignup(deps), "marco@example.com\n", pollTicker)
	require.NoError(t, err)
	assert.Contains(t, *openedURL, "http://localhost:3000/signup")
	assert.Contains(t, *openedURL, "email=marco%40example.com")
}

func TestSignupHonorsWebURLOverride(t *testing.T) {
	setAuthTestHome(t)
	// An explicit VOLCANO_WEB_URL overrides the API-derived origin.
	t.Setenv("VOLCANO_WEB_URL", "https://custom.example")
	deps, openedURL, pollTicker := signupBrowserDeps(t)
	deps.GitCommandRunner = cliruntime.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ted@example.com\n"), nil
	})

	_, err := executeAuthCommandWithInputAndTick(t, NewSignup(deps), "\n", pollTicker)
	require.NoError(t, err)
	assert.Equal(t, authTestSignupOverrideURL, *openedURL)
}

func TestPromptSignupEmailRejectsInvalid(t *testing.T) {
	deps := cliruntime.Deps{GitCommandRunner: cliruntime.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("no git email")
	})}
	var out bytes.Buffer
	_, err := promptSignupEmail(context.Background(), deps, bufio.NewReader(bytes.NewBufferString("not-an-email\n")), &out)
	require.ErrorContains(t, err, "invalid email address")
}

func TestPromptSignupEmailRequiresEmail(t *testing.T) {
	deps := cliruntime.Deps{GitCommandRunner: cliruntime.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("no git email")
	})}
	var out bytes.Buffer
	_, err := promptSignupEmail(context.Background(), deps, bufio.NewReader(bytes.NewBufferString("\n")), &out)
	require.ErrorContains(t, err, "email address is required")
}

func executeAuthCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	return executeAuthCommandWithInput(t, cmd, "", args...)
}

// executeLogin runs login against server and returns stdout and stderr apart.
func executeLogin(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	return executeLoginWithDeps(t, cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}, args...)
}

func executeLoginWithDeps(t *testing.T, deps cliruntime.Deps, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewLogin(deps)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func readAuthFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func executeAuthCommandWithInput(t *testing.T, cmd *cobra.Command, input string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetIn(bytes.NewBufferString(input))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func executeAuthCommandWithInputAndTick(t *testing.T, cmd *cobra.Command, input string, ticker *authCmdFakeTicker, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetIn(bytes.NewBufferString(input))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	done := make(chan error, 1)
	go func() { done <- cmd.Execute() }()
	ticker.tick()
	select {
	case err := <-done:
		return out.String(), err
	case <-time.After(2 * time.Second):
		t.Fatal("command did not complete")
		return out.String(), nil
	}
}

func signupBrowserDeps(t *testing.T) (cliruntime.Deps, *string, *authCmdFakeTicker) {
	t.Helper()
	pollTicker := newAuthCmdFakeTicker()
	dotTicker := newAuthCmdFakeTicker()
	timeoutTimer := newAuthCmdFakeTicker()
	openedURL := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/device/authorize":
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"device_code":               "device-code",
				"user_code":                 "ABCD-EFGH",
				"verification_uri":          "https://volcano.dev/device",
				"verification_uri_complete": "https://volcano.dev/device?user_code=ABCD-EFGH",
				"expires_in":                120,
				"interval":                  1,
			})
		case "/auth/device/token":
			writeAuthJSON(t, w, http.StatusOK, map[string]any{"access_token": "auth-access-token"})
		case "/auth/platform/exchange":
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"token":      "platform-token",
				"user_id":    "platform-user-1",
				"token_id":   "33333333-3333-4333-8333-333333333333",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		OpenBrowser: func(rawURL string) error {
			openedURL = rawURL
			return nil
		},
		NewTimer: func(time.Duration) cliruntime.Timer { return timeoutTimer },
		NewTicker: func(time.Duration) cliruntime.Ticker {
			if !pollTicker.created {
				pollTicker.created = true
				return pollTicker
			}
			return dotTicker
		},
	}, &openedURL, pollTicker
}

type authCmdFakeTicker struct {
	created bool
	ch      chan time.Time
}

func newAuthCmdFakeTicker() *authCmdFakeTicker {
	return &authCmdFakeTicker{ch: make(chan time.Time, 1)}
}

func (t *authCmdFakeTicker) C() <-chan time.Time { return t.ch }
func (t *authCmdFakeTicker) Stop()               {}
func (t *authCmdFakeTicker) Reset(time.Duration) {}
func (t *authCmdFakeTicker) tick()               { t.ch <- time.Now() }

func setAuthTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_WEB_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveAuthTestConfig(t *testing.T, cfg *cliconfig.Config) {
	t.Helper()
	require.NoError(t, cfg.Save())
}

func loadAuthTestConfig(t *testing.T) *cliconfig.Config {
	t.Helper()
	cfg, err := cliconfig.LoadWithoutLink()
	require.NoError(t, err)
	return cfg
}

func writeAuthJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func TestLogoutRemovesUnparseableConfig(t *testing.T) {
	setAuthTestHome(t)
	path, err := cliconfig.Path()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"user_token": "token"`), 0o600))

	_, err = executeAuthCommand(t, NewLogout())
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "a damaged config with a token is removed: %v", err)
}

func TestLogoutRemovesUnreadableConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not block reads on Windows")
	}
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "token"})
	path, err := cliconfig.Path()
	require.NoError(t, err)
	require.NoError(t, os.Chmod(path, 0o000))
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("running as a user that can read any file")
	}

	_, err = executeAuthCommand(t, NewLogout())
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "an unreadable config with a token is removed: %v", err)
}
