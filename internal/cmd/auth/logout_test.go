package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// revokeAPI answers DELETE /user/tokens/current with status and records each
// request's method, path, and bearer token.
type revokeAPI struct {
	status int
	mu     sync.Mutex
	calls  []string
}

func newRevokeAPI(t *testing.T, status int) (*revokeAPI, cliruntime.Deps) {
	t.Helper()
	api := &revokeAPI{status: status}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		api.calls = append(api.calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
		api.mu.Unlock()
		if r.Method != http.MethodDelete || r.URL.Path != "/user/tokens/current" {
			writeAuthJSON(t, w, http.StatusNotFound, map[string]any{"error": "not found"})
			return
		}
		if api.status == http.StatusNoContent {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeAuthJSON(t, w, api.status, map[string]any{"error": http.StatusText(api.status)})
	}))
	t.Cleanup(server.Close)
	return api, cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
}

func (a *revokeAPI) requests() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

func requireLoggedOut(t *testing.T) {
	t.Helper()
	assert.Empty(t, loadAuthTestConfig(t).UserToken, "the login is removed from this machine")
}

func TestLogoutRevokesTheSavedLoginOnTheServer(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved", UserID: "user"})
	api, deps := newRevokeAPI(t, http.StatusNoContent)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Equal(t, []string{"DELETE /user/tokens/current Bearer pk-saved"}, api.requests())
	assert.Contains(t, out, "Logged out: this login's token and session were revoked on the server")
	assert.Contains(t, out, "Removed the login from ~/.volcano/config.json.")
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
		UserToken:       "pk-saved",
		UserID:          "user",
		CurrentProject:  &cliconfig.ProjectConfig{ID: "p1", Name: "app"},
		FunctionAliases: map[string]map[string]string{"scope": {"a": "f1"}},
		DocsSource:      docs,
		Telemetry:       telemetry,
	})
	_, deps := newRevokeAPI(t, http.StatusNoContent)

	_, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Equal(t, &cliconfig.Config{DocsSource: docs, Telemetry: telemetry}, loadAuthTestConfig(t))
}

// VOLCANO_TOKEN was never saved, so logout revokes the saved login rather than
// the one in the environment, and says the environment's stays in use.
func TestLogoutRevokesTheSavedLoginNotVolcanoToken(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
	t.Setenv("VOLCANO_TOKEN", "pk-from-env")
	api, deps := newRevokeAPI(t, http.StatusNoContent)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Equal(t, []string{"DELETE /user/tokens/current Bearer pk-saved"}, api.requests())
	assert.Contains(t, out, "VOLCANO_TOKEN is still set, so commands keep using it. Logout neither revokes nor unsets it.")
}

// A token the server no longer accepts was revoked elsewhere or expired, so
// the login has already ended there.
func TestLogoutWhenTheServerAlreadyEndedTheLogin(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
	_, deps := newRevokeAPI(t, http.StatusUnauthorized)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Contains(t, out, "the server had already ended this login, so there was nothing to revoke")
	requireLoggedOut(t)
}

// A local-mode server keeps no sign-in session, so there is nothing to revoke
// and only this machine's login is removed.
func TestLogoutWhereTheServerKeepsNoSession(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
	_, deps := newRevokeAPI(t, http.StatusNotImplemented)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Contains(t, out, "this server keeps no sign-in session, so there was nothing to revoke")
	assert.Contains(t, out, "Removed the login from ~/.volcano/config.json.")
	requireLoggedOut(t)
}

// A revoke that can pass keeps the login saved, so the command says to run
// logout again and exits non-zero.
func TestLogoutKeepsTheLoginWhenTheRevokeCanPass(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			setAuthTestHome(t)
			saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
			_, deps := newRevokeAPI(t, status)

			_, err := executeAuthCommand(t, NewLogout(deps))
			require.Error(t, err)

			requireLogoutCanBeRetried(t, err)
		})
	}
}

func TestLogoutKeepsTheLoginWhenTheServerCannotBeReached(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()

	_, err := executeAuthCommand(t, NewLogout(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}))
	require.Error(t, err)

	requireLogoutCanBeRetried(t, err)
}

func requireLogoutCanBeRetried(t *testing.T, err error) {
	t.Helper()
	assert.Contains(t, err.Error(), "the server did not end this login")
	assert.Contains(t, err.Error(), "The login is still saved on this machine. Run volcano logout again")
	assert.NotContains(t, err.Error(), "dashboard")
	assert.Equal(t, "pk-saved", loadAuthTestConfig(t).UserToken, "the login stays saved for another try")
}

func TestLogoutRemovesTheLoginWhenTheServerRefuses(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			setAuthTestHome(t)
			saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
			_, deps := newRevokeAPI(t, status)

			_, err := executeAuthCommand(t, NewLogout(deps))
			require.Error(t, err)

			assert.Contains(t, err.Error(), "logged out of this machine, but the server did not end this login")
			assert.Contains(t, err.Error(), "The token stays valid until it expires")
			assert.NotContains(t, err.Error(), "dashboard")
			requireLoggedOut(t)
		})
	}
}

// Revoking the calling token covers account tokens only, so a project access
// token is removed from this machine without asking, and the command names the
// one that revokes it.
func TestLogoutLeavesAProjectAccessTokenToAccessTokensRevoke(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: cliconfig.ProjectTokenPrefix + "saved"})
	api, deps := newRevokeAPI(t, http.StatusNoContent)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Empty(t, api.requests())
	assert.Contains(t, out, "The project access token stays valid: revoke it with volcano cloud access-tokens revoke.")
	requireLoggedOut(t)
}

func TestLogoutWithNothingSaved(t *testing.T) {
	setAuthTestHome(t)
	api, deps := newRevokeAPI(t, http.StatusNoContent)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Empty(t, api.requests())
	assert.Contains(t, out, "Logged out: no login was saved, so nothing was revoked")
}

// After `volcano login --token "$VOLCANO_TOKEN"` the saved token and the
// environment's are the same, so revoking one ends both.
func TestLogoutSaysVolcanoTokenEndedWhenItIsTheSavedToken(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
	t.Setenv("VOLCANO_TOKEN", "pk-saved\n")
	_, deps := newRevokeAPI(t, http.StatusNoContent)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Contains(t, out, "VOLCANO_TOKEN holds the token this login used, which the server no longer accepts, "+
		"so commands that use it now fail. Unset it.")
	assert.NotContains(t, out, "commands keep using it")
}

// Logout never uses the project link, so a broken one in the working directory
// must not stop it from reading and revoking the saved login.
func TestLogoutRevokesDespiteABadProjectLink(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
	t.Chdir(t.TempDir())
	require.NoError(t, os.MkdirAll(".volcano", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(".volcano", "project.json"), []byte("<<<<<<< HEAD\n"), 0o644))
	api, deps := newRevokeAPI(t, http.StatusNoContent)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Equal(t, []string{"DELETE /user/tokens/current Bearer pk-saved"}, api.requests())
	assert.Contains(t, out, "Logged out: this login's token and session were revoked on the server")
	requireLoggedOut(t)
}

// When the config can't be removed, the command says what the server did and
// that the login is still on this machine, never that it logged out.
func TestLogoutSaysSoWhenTheLoginCannotBeRemoved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	for _, tc := range []struct {
		status int
		want   string
	}{
		{http.StatusNoContent, "the server ended this login, but removing it from this machine failed"},
		{http.StatusNotFound, "the server did not end this login (HTTP 404: Not Found), and removing it from this machine failed"},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			setAuthTestHome(t)
			saveAuthTestConfig(t, &cliconfig.Config{UserToken: "pk-saved"})
			path, err := cliconfig.Path()
			require.NoError(t, err)
			// The config's lock file points into a directory nobody can write,
			// so reading the login works and removing it can't take the lock.
			readOnly := t.TempDir()
			require.NoError(t, os.Chmod(readOnly, 0o500))
			t.Cleanup(func() { _ = os.Chmod(readOnly, 0o700) })
			if f, err := os.Create(filepath.Join(readOnly, "probe")); err == nil {
				_ = f.Close()
				t.Skip("running as a user that can write any directory")
			}
			require.NoError(t, os.Remove(path+".lock"))
			require.NoError(t, os.Symlink(filepath.Join(readOnly, "lock"), path+".lock"))
			_, deps := newRevokeAPI(t, tc.status)

			out, err := executeAuthCommand(t, NewLogout(deps))
			require.Error(t, err)

			assert.Contains(t, err.Error(), tc.want)
			assert.NotContains(t, out, "Logged out")
			assert.NotContains(t, err.Error(), "logged out of this machine")
		})
	}
}

func TestLogoutRemovesUnparseableConfig(t *testing.T) {
	setAuthTestHome(t)
	path, err := cliconfig.Path()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(`{"user_token": "token"`), 0o600))
	api, deps := newRevokeAPI(t, http.StatusNoContent)

	out, err := executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	assert.Empty(t, api.requests(), "no token could be read to revoke")
	assert.Contains(t, out, "The saved login could not be read, so nothing was revoked on the server")
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
	_, deps := newRevokeAPI(t, http.StatusNoContent)

	_, err = executeAuthCommand(t, NewLogout(deps))
	require.NoError(t, err)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "an unreadable config with a token is removed: %v", err)
}
