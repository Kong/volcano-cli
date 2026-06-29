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
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	authProjectID     = "11111111-1111-4111-8111-111111111111"
	authTestSignupURL = "http://localhost:3000/signup?email=ted%40example.com&next=%2Fdevice%3Fuser_code%3DABCD-EFGH&source=cli"
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

	out, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "--token", "valid-token")
	require.NoError(t, err)
	assert.Equal(t, "Bearer valid-token", sawAuth)
	assert.Contains(t, out, "Token validated")
	assert.Contains(t, out, "Logged in successfully")

	cfg := loadAuthTestConfig(t)
	assert.Equal(t, "valid-token", cfg.UserToken)
	assert.Empty(t, cfg.UserID)
}

func TestLoginTokenInvalidFailsWithoutSavingConfig(t *testing.T) {
	setAuthTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}))
	defer server.Close()

	_, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "--token", "bad-token")
	require.ErrorContains(t, err, "token authentication failed: invalid token")

	path, err := cliconfig.Path()
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "config should not be saved after invalid token, stat err: %v", err)
}

func TestLogoutDeletesConfig(t *testing.T) {
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

func TestSignupUsesGitEmailDefault(t *testing.T) {
	setAuthTestHome(t)
	t.Setenv("VOLCANO_WEB_URL", "http://localhost:3000")
	deps, openedURL, pollTicker := signupBrowserDeps(t)
	deps.GitCommandRunner = cliruntime.CommandRunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		assert.Equal(t, "git", name)
		assert.Equal(t, []string{"config", "--global", "user.email"}, args)
		return []byte("ted@example.com\n"), nil
	})

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

func TestSignupAllowsEmailOverride(t *testing.T) {
	setAuthTestHome(t)
	t.Setenv("VOLCANO_WEB_URL", "http://localhost:3000")
	deps, openedURL, pollTicker := signupBrowserDeps(t)
	deps.GitCommandRunner = cliruntime.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return []byte("ted@example.com\n"), nil
	})

	_, err := executeAuthCommandWithInputAndTick(t, NewSignup(deps), "marco@example.com\n", pollTicker)
	require.NoError(t, err)
	assert.Contains(t, *openedURL, "email=marco%40example.com")
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
	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	return cfg
}

func writeAuthJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
