package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	authAlphaProjectID = "11111111-1111-4111-8111-111111111111"
)

func TestLoginWithTokenSuccess(t *testing.T) {
	cfg := testAuthConfig(t)
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/projects", r.URL.Path)
		sawAuth = r.Header.Get("Authorization")
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				map[string]any{
					"id": authAlphaProjectID,
				},
			},
			"has_more": false,
			"page":     1,
			"limit":    100,
			"total":    0,
		})
	}))
	defer server.Close()

	credentials, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).LoginWithToken(context.Background(), cfg, "  valid-token\n", "")
	require.NoError(t, err)
	assert.Equal(t, "Bearer valid-token", sawAuth)
	assert.Equal(t, Credentials{Token: "valid-token", APIURL: server.URL}, credentials)
}

func TestLoginWithProjectTokenValidatesAgainstTheProject(t *testing.T) {
	cfg := testAuthConfig(t)
	var sawPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"id":         authAlphaProjectID,
			"name":       "Alpha",
			"status":     "active",
			"created_at": "2026-05-20T00:00:00Z",
			"updated_at": "2026-05-20T00:00:00Z",
		})
	}))
	defer server.Close()

	credentials, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, "pt-project-token", authAlphaProjectID)
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+authAlphaProjectID, sawPath)
	require.NotNil(t, credentials.Project)
	assert.Equal(t, authAlphaProjectID, credentials.Project.ID)
	assert.Equal(t, "Alpha", credentials.Project.Name)
}

func TestLoginWithProjectTokenFallsBackToTheSelectedProject(t *testing.T) {
	cfg := testAuthConfig(t)
	cfg.CurrentProject = &config.ProjectConfig{ID: authAlphaProjectID, Name: "Alpha"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/projects/"+authAlphaProjectID, r.URL.Path)
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"id":         authAlphaProjectID,
			"name":       "Alpha",
			"status":     "active",
			"created_at": "2026-05-20T00:00:00Z",
			"updated_at": "2026-05-20T00:00:00Z",
		})
	}))
	defer server.Close()

	credentials, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, "pt-project-token", "")
	require.NoError(t, err)
	require.NotNil(t, credentials.Project)
	assert.Equal(t, authAlphaProjectID, credentials.Project.ID)
}

func TestLoginWithProjectTokenRequiresAProject(t *testing.T) {
	cfg := testAuthConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request", r.URL.Path)
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, "pt-project-token", "")
	require.ErrorContains(t, err, "--project <project-id>")
}

func TestLoginWithProjectTokenForTheWrongProject(t *testing.T) {
	cfg := testAuthConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, "pt-project-token", authAlphaProjectID)
	require.ErrorContains(t, err, "token is not valid for project "+authAlphaProjectID)
	assert.NotContains(t, err.Error(), "taken from", "the user named this project, so there is nothing to explain")
}

// The fallback reaches the project a previous `volcano use` saved, which a
// user logging in with a new credential never mentioned. Reporting it as
// "not valid for project <id>" names a project out of nowhere, and reads as
// the token being wrong rather than the selection being stale.
func TestLoginWithProjectTokenNamesWhereAnUnmentionedProjectCameFrom(t *testing.T) {
	cfg := testAuthConfig(t)
	cfg.CurrentProject = &config.ProjectConfig{ID: authAlphaProjectID, Name: "Alpha"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, "pt-project-token", "")
	require.ErrorContains(t, err, "token is not valid for project "+authAlphaProjectID)
	require.ErrorContains(t, err, "taken from the project 'volcano use' last selected")
	require.ErrorContains(t, err, "pass --project")
}

func TestLoginWithProjectTokenNamesTheLinkAsTheProjectSource(t *testing.T) {
	cfg := testAuthConfig(t)
	cfg.CurrentProject = &config.ProjectConfig{ID: "22222222-2222-4222-8222-222222222222", Name: "Beta"}
	linkDir := t.TempDir()
	cfg.Link = &config.Link{Dir: linkDir, Project: config.ProjectConfig{ID: authAlphaProjectID, Name: "Alpha"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusForbidden, map[string]string{"error": "forbidden"})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, "pt-project-token", "")
	require.ErrorContains(t, err, "token is not valid for project "+authAlphaProjectID)
	require.ErrorContains(t, err, "taken from this directory's project link, "+filepath.Join(linkDir, ".volcano", "project.json"))
}

func TestLoginWithProjectTokenNamesTheEnvironmentProject(t *testing.T) {
	cfg := testAuthConfig(t)
	t.Setenv("VOLCANO_PROJECT_ID", authAlphaProjectID)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusNotFound, map[string]string{"error": "not found"})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, "pt-project-token", "")
	require.ErrorContains(t, err, "taken from VOLCANO_PROJECT_ID")
}

// `volcano use` takes a name, so --project has to mean the same thing. An
// account token can list projects, which is what resolving a name needs.
func TestLoginWithAccountTokenResolvesTheProjectByName(t *testing.T) {
	cfg := testAuthConfig(t)
	var listed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/projects", r.URL.Path)
		listed = true
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{map[string]any{
				"id":         authAlphaProjectID,
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

	credentials, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, config.AccountTokenPrefix+"account-token", "my-app")
	require.NoError(t, err)
	assert.True(t, listed)
	require.NotNil(t, credentials.Project)
	assert.Equal(t, authAlphaProjectID, credentials.Project.ID)
	assert.Equal(t, "my-app", credentials.Project.Name)
}

func TestLoginWithAccountTokenReportsAnUnknownProjectName(t *testing.T) {
	cfg := testAuthConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{}, "has_more": false, "page": 1, "limit": 100, "total": 0,
		})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, config.AccountTokenPrefix+"account-token", "my-app")
	require.ErrorContains(t, err, "project not found: my-app")
}

// A project access token cannot list projects, so there is nothing to resolve
// a name against. Saying that beats the uuid parser's "invalid UUID length: 6".
func TestLoginWithProjectTokenRejectsAProjectName(t *testing.T) {
	cfg := testAuthConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		assert.Fail(t, "unexpected request for a name a project token cannot resolve", r.URL.Path)
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).
		LoginWithToken(context.Background(), cfg, "pt-project-token", "my-app")
	require.ErrorContains(t, err, "cannot look up a project by name")
	assert.NotContains(t, err.Error(), "invalid UUID length")
}

func TestLoginWithTokenInvalid(t *testing.T) {
	cfg := testAuthConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).LoginWithToken(context.Background(), cfg, "bad-token", "")
	require.EqualError(t, err, "invalid token")
	assert.Equal(t, http.StatusUnauthorized, api.Status(err), "a rejected token keeps the API's 401 for the exit code")
}

// authDeviceCode is the device code the dashboard put in the prompt. It is
// distinctive so a test can check it never shows up anywhere.
const authDeviceCode = "dc-dashboard-secret-7f3a"

// deviceCodeServer answers the device token poll with each of polls in turn,
// repeating the last, and the platform exchange and token check after it. It
// fails the test on device authorization: the dashboard already did that.
type deviceCodeServer struct {
	t     *testing.T
	polls []func(w http.ResponseWriter)
	// listStatus makes the token check fail; 0 answers it.
	listStatus int
	pollCount  atomic.Int32
	// exchangeStatuses answer the platform exchange in turn before it
	// succeeds.
	exchangeStatuses []int
	exchanges        atomic.Int32
}

func (d *deviceCodeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/auth/device/token":
		var body map[string]string
		assert.NoError(d.t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(d.t, localmode.DeviceClientID, body["client_id"])
		assert.Equal(d.t, authDeviceCode, body["device_code"])
		n := int(d.pollCount.Add(1))
		d.polls[min(n, len(d.polls))-1](w)
	case "/auth/platform/exchange":
		assert.Equal(d.t, "Bearer auth-access-token", r.Header.Get("Authorization"))
		if n := int(d.exchanges.Add(1)); n <= len(d.exchangeStatuses) {
			writeAuthJSON(d.t, w, d.exchangeStatuses[n-1], map[string]string{"error": "failed"})
			return
		}
		writeAuthJSON(d.t, w, http.StatusOK, map[string]any{
			"token": "pk-device", "user_id": "user-1", "token_id": "33333333-3333-4333-8333-333333333333",
			"expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	case "/projects":
		if d.listStatus != 0 {
			writeAuthJSON(d.t, w, d.listStatus, map[string]string{"error": "failed"})
			return
		}
		assert.Equal(d.t, "Bearer pk-device", r.Header.Get("Authorization"))
		writeAuthJSON(d.t, w, http.StatusOK, map[string]any{"data": []any{}, "has_more": false, "page": 1, "limit": 1, "total": 0})
	case "/projects/" + authAlphaProjectID:
		writeAuthJSON(d.t, w, http.StatusNotFound, map[string]string{"error": "project not found"})
	default:
		d.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}
}

func approvedPoll(t *testing.T) func(http.ResponseWriter) {
	t.Helper()
	return func(w http.ResponseWriter) {
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"access_token": "auth-access-token", "token_type": "bearer", "expires_in": 3600, "refresh_token": "auth-refresh-token",
		})
	}
}

func oauthErrorPoll(t *testing.T, code, description string) func(http.ResponseWriter) {
	t.Helper()
	return func(w http.ResponseWriter) {
		writeAuthJSON(t, w, http.StatusBadRequest, map[string]string{"error": code, "error_description": description})
	}
}

// deviceCodeClock hands out fake timers and tickers for a device-code login,
// and checks the durations it asks for.
type deviceCodeClock struct {
	timeout, poll, dot *authFakeTicker
}

func newDeviceCodeClock(t *testing.T) (*deviceCodeClock, cliruntime.Deps) {
	t.Helper()
	clock := &deviceCodeClock{timeout: newAuthFakeTicker(), poll: newAuthFakeTicker(), dot: newAuthFakeTicker()}
	return clock, cliruntime.Deps{
		OpenBrowser: func(string) error {
			t.Error("a device code from the dashboard never opens the browser")
			return nil
		},
		NewTimer: func(duration time.Duration) cliruntime.Timer {
			assert.Equal(t, 10*time.Minute, duration)
			return clock.timeout
		},
		NewTicker: func(duration time.Duration) cliruntime.Ticker {
			switch duration {
			case 5 * time.Second:
				return clock.poll
			case time.Second:
				return clock.dot
			}
			require.FailNowf(t, "unexpected ticker duration", "%s", duration)
			return nil
		},
	}
}

func deviceCodeDeps(deps cliruntime.Deps, server *httptest.Server) cliruntime.Deps {
	deps.HTTPClient = server.Client()
	deps.APIBaseURL = server.URL
	return deps
}

// The dashboard approved the code before the user pasted the prompt, so the
// first poll, made before any tick, signs in.
func TestLoginWithDeviceCodeSignsInOnTheFirstPoll(t *testing.T) {
	cfg := testAuthConfig(t)
	fake := &deviceCodeServer{t: t, polls: []func(http.ResponseWriter){approvedPoll(t)}}
	server := httptest.NewServer(fake)
	defer server.Close()
	_, deps := newDeviceCodeClock(t)

	var out bytes.Buffer
	credentials, err := NewService(deviceCodeDeps(deps, server)).
		LoginWithDeviceCode(context.Background(), cfg, " "+authDeviceCode+"\n", "", &out)
	require.NoError(t, err)
	assert.Equal(t, Credentials{Token: "pk-device", UserID: "user-1", APIURL: server.URL}, credentials)
	assert.Equal(t, int32(1), fake.pollCount.Load())
	assert.Equal(t, "Signing in with the code from the dashboard\n", out.String())

	loaded, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "pk-device", loaded.UserToken)
	assert.Equal(t, "user-1", loaded.UserID)
	assertDeviceCodeNotSaved(t)
}

func TestLoginWithDeviceCodeWaitsWhileAuthorizationIsPending(t *testing.T) {
	cfg := testAuthConfig(t)
	fake := &deviceCodeServer{t: t, polls: []func(http.ResponseWriter){
		oauthErrorPoll(t, "authorization_pending", "authorization is still pending"),
		oauthErrorPoll(t, "authorization_pending", "authorization is still pending"),
		approvedPoll(t),
	}}
	server := httptest.NewServer(fake)
	defer server.Close()
	clock, deps := newDeviceCodeClock(t)
	clock.poll.tick()
	clock.poll.tick()

	credentials, err := NewService(deviceCodeDeps(deps, server)).
		LoginWithDeviceCode(context.Background(), cfg, authDeviceCode, "", &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, "pk-device", credentials.Token)
	assert.Equal(t, int32(3), fake.pollCount.Load())
}

// Hosting answers every unknown or used code with invalid_grant and one
// generic description, so the CLI can't say which it was.
func TestLoginWithDeviceCodeNamesWhyItWasRefused(t *testing.T) {
	for _, tc := range []struct {
		code, description, reason string
	}{
		{"expired_token", "device code expired", "has expired"},
		{"access_denied", "The resource owner or authorization server denied the request.", "was denied: The resource owner or authorization server denied the request"},
		{"invalid_grant", "The provided authorization grant (e.g., authorization code, resource owner credentials) or " +
			"refresh token is invalid, expired, revoked, does not match the redirection URI used in the authorization " +
			"request, or was issued to another client.", "is not valid or was already used"},
		{"unsupported_grant_type", "unsupported grant " + authDeviceCode, "was refused"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			cfg := testAuthConfig(t)
			fake := &deviceCodeServer{t: t, polls: []func(http.ResponseWriter){oauthErrorPoll(t, tc.code, tc.description)}}
			server := httptest.NewServer(fake)
			defer server.Close()
			_, deps := newDeviceCodeClock(t)

			var out bytes.Buffer
			_, err := NewService(deviceCodeDeps(deps, server)).
				LoginWithDeviceCode(context.Background(), cfg, authDeviceCode, "", &out)
			refused, ok := errors.AsType[*DeviceCodeRefusedError](err)
			require.True(t, ok, "got %v", err)
			assert.Equal(t, tc.reason, refused.Reason)
			assert.Equal(t, "the code from the dashboard "+tc.reason+
				". Copy a new prompt from the dashboard, or sign in in the browser", err.Error())
			require.ErrorIs(t, err, config.ErrNotAuthenticated, "a refused code exits as not signed in")
			assert.NotContains(t, out.String(), authDeviceCode)

			loaded, err := config.Load()
			require.NoError(t, err)
			assert.Empty(t, loaded.UserToken, "nothing is saved")
		})
	}
}

// The API answers 403 access_denied when the account's email domain isn't
// allowed. That ends the login like any refusal, without retrying.
func TestLoginWithDeviceCodeTreatsA403AsARefusal(t *testing.T) {
	cfg := testAuthConfig(t)
	fake := &deviceCodeServer{t: t, polls: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		writeAuthJSON(t, w, http.StatusForbidden, map[string]string{"error": "access_denied", "error_description": "email domain not allowed"})
	}}}
	server := httptest.NewServer(fake)
	defer server.Close()
	_, deps := newDeviceCodeClock(t)

	_, err := NewService(deviceCodeDeps(deps, server)).
		LoginWithDeviceCode(context.Background(), cfg, authDeviceCode, "", &bytes.Buffer{})
	refused, ok := errors.AsType[*DeviceCodeRefusedError](err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, "was denied: email domain not allowed", refused.Reason)
	assert.Equal(t, int32(1), fake.pollCount.Load())
}

// The code is spent once the poll returns a token, so a failed exchange after
// it is retried rather than losing the login.
func TestLoginWithDeviceCodeRetriesAFailedExchange(t *testing.T) {
	setRetryDelay(t)
	cfg := testAuthConfig(t)
	fake := &deviceCodeServer{
		t: t, polls: []func(http.ResponseWriter){approvedPoll(t)},
		exchangeStatuses: []int{http.StatusBadGateway, http.StatusServiceUnavailable},
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	_, deps := newDeviceCodeClock(t)

	credentials, err := NewService(deviceCodeDeps(deps, server)).
		LoginWithDeviceCode(context.Background(), cfg, authDeviceCode, "", &bytes.Buffer{})
	require.NoError(t, err)
	assert.Equal(t, "pk-device", credentials.Token)
	assert.Equal(t, int32(3), fake.exchanges.Load())
}

// A 4xx from the exchange is an answer, not a blip, so it isn't retried.
func TestLoginWithDeviceCodeDoesNotRetryARefusedExchange(t *testing.T) {
	setRetryDelay(t)
	cfg := testAuthConfig(t)
	fake := &deviceCodeServer{
		t: t, polls: []func(http.ResponseWriter){approvedPoll(t)},
		exchangeStatuses: []int{http.StatusForbidden},
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	_, deps := newDeviceCodeClock(t)

	_, err := NewService(deviceCodeDeps(deps, server)).
		LoginWithDeviceCode(context.Background(), cfg, authDeviceCode, "", &bytes.Buffer{})
	require.ErrorContains(t, err, "failed to exchange platform token")
	assert.Equal(t, int32(1), fake.exchanges.Load())
}

func setRetryDelay(t *testing.T) {
	t.Helper()
	original := platformExchangeRetryDelay
	platformExchangeRetryDelay = time.Millisecond
	t.Cleanup(func() { platformExchangeRetryDelay = original })
}

func TestLoginWithDeviceCodeStillPendingWhenTheCodeExpiresIsRefused(t *testing.T) {
	cfg := testAuthConfig(t)
	fake := &deviceCodeServer{t: t, polls: []func(http.ResponseWriter){
		oauthErrorPoll(t, "authorization_pending", "authorization is still pending"),
	}}
	server := httptest.NewServer(fake)
	defer server.Close()
	clock, deps := newDeviceCodeClock(t)
	clock.timeout.tick()

	_, err := NewService(deviceCodeDeps(deps, server)).
		LoginWithDeviceCode(context.Background(), cfg, authDeviceCode, "", &bytes.Buffer{})
	refused, ok := errors.AsType[*DeviceCodeRefusedError](err)
	require.True(t, ok, "got %v", err)
	assert.Equal(t, "has expired", refused.Reason)
}

// The API spends the code when it issues the token, so the login is saved
// even when the check after it fails, and the failure keeps its API status
// for the exit code.
func TestLoginWithDeviceCodeSavesTheLoginWhenALaterCheckFails(t *testing.T) {
	for name, tc := range map[string]struct {
		project    string
		listStatus int
		status     int
	}{
		"project not found": {project: authAlphaProjectID, status: http.StatusNotFound},
		"validation fails":  {listStatus: http.StatusInternalServerError, status: http.StatusInternalServerError},
	} {
		t.Run(name, func(t *testing.T) {
			cfg := testAuthConfig(t)
			fake := &deviceCodeServer{t: t, polls: []func(http.ResponseWriter){approvedPoll(t)}, listStatus: tc.listStatus}
			server := httptest.NewServer(fake)
			defer server.Close()
			_, deps := newDeviceCodeClock(t)

			_, err := NewService(deviceCodeDeps(deps, server)).
				LoginWithDeviceCode(context.Background(), cfg, authDeviceCode, tc.project, &bytes.Buffer{})
			_, saved := errors.AsType[*LoginSavedError](err)
			require.True(t, saved, "got %v", err)
			assert.Equal(t, tc.status, api.Status(err))

			loaded, err := config.Load()
			require.NoError(t, err)
			assert.Equal(t, "pk-device", loaded.UserToken)
			assert.Equal(t, "user-1", loaded.UserID)
			assert.Nil(t, loaded.CurrentProject, "a project that failed the check is not selected")
		})
	}
}

// An API that echoes the code in a failure never gets it printed.
func TestLoginWithDeviceCodeRedactsTheCodeInErrors(t *testing.T) {
	cfg := testAuthConfig(t)
	fake := &deviceCodeServer{t: t, polls: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		writeAuthJSON(t, w, http.StatusInternalServerError, map[string]string{"error": "no grant for " + authDeviceCode})
	}}}
	server := httptest.NewServer(fake)
	defer server.Close()
	clock, deps := newDeviceCodeClock(t)
	clock.poll.tick()
	clock.poll.tick()

	_, err := NewService(deviceCodeDeps(deps, server)).
		LoginWithDeviceCode(context.Background(), cfg, authDeviceCode, "", &bytes.Buffer{})
	require.ErrorContains(t, err, "failed to poll device token")
	assert.Contains(t, err.Error(), "[redacted]")
	assert.NotContains(t, err.Error(), authDeviceCode)
	assert.Equal(t, http.StatusInternalServerError, api.Status(err), "the redacted error keeps the API status")
}

// assertDeviceCodeNotSaved checks the config file never holds the device code.
func assertDeviceCodeNotSaved(t *testing.T) {
	t.Helper()
	path, err := config.Path()
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), authDeviceCode)
}

func TestLoginWithBrowserDeviceFlow(t *testing.T) {
	cfg := testAuthConfig(t)
	// httptest binds to 127.0.0.1, so resolveDeviceClientID issues the
	// deterministic local-mode client id regardless of any configured value.

	var pollCount atomic.Int32
	var openedURL string
	var exchangeAuth string
	timeoutTimer := newAuthFakeTicker()
	pollTicker := newAuthFakeTicker()
	dotTicker := newAuthFakeTicker()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimSuffix(r.URL.Path, "/") {
		case "/auth/device/authorize":
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, localmode.DeviceClientID, body["client_id"])
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"device_code":               "device-code",
				"user_code":                 "ABCD-EFGH",
				"verification_uri":          "https://volcano.dev/device",
				"verification_uri_complete": "https://volcano.dev/device?user_code=ABCD-EFGH",
				"expires_in":                120,
				"interval":                  1,
			})
		case "/auth/device/token":
			switch pollCount.Add(1) {
			case 1:
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("temporary failure"))
				return
			case 2:
				writeAuthJSON(t, w, http.StatusBadRequest, map[string]string{"error": "authorization_pending"})
				return
			}
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"access_token":  "auth-access-token",
				"token_type":    "bearer",
				"expires_in":    3600,
				"refresh_token": "auth-refresh-token",
			})
		case "/auth/platform/exchange":
			exchangeAuth = r.Header.Get("Authorization")
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"token":      "platform-token",
				"user_id":    "platform-user-1",
				"token_id":   "33333333-3333-4333-8333-333333333333",
				"expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		OpenBrowser: func(url string) error {
			openedURL = url
			return nil
		},
		NewTimer: func(duration time.Duration) cliruntime.Timer {
			assert.Equal(t, 120*time.Second, duration)
			return timeoutTimer
		},
		NewTicker: func(duration time.Duration) cliruntime.Ticker {
			switch duration {
			case time.Second:
				if pollTicker.created.CompareAndSwap(false, true) {
					return pollTicker
				}
				return dotTicker
			default:
				require.FailNowf(t, "unexpected ticker duration", "%s", duration)
				return nil
			}
		},
	}

	var out bytes.Buffer
	done := make(chan struct {
		credentials Credentials
		err         error
	}, 1)
	go func() {
		credentials, err := NewService(deps).LoginWithBrowser(context.Background(), cfg, &out)
		done <- struct {
			credentials Credentials
			err         error
		}{credentials: credentials, err: err}
	}()

	dotTicker.tick()
	pollTicker.tick()
	pollTicker.tick()
	pollTicker.tick()

	result := <-done
	require.NoError(t, result.err, "output:\n%s", out.String())
	assert.Equal(t, Credentials{Token: "platform-token", UserID: "platform-user-1", APIURL: server.URL}, result.credentials)
	assert.Equal(t, "https://volcano.dev/device?user_code=ABCD-EFGH", openedURL)
	assert.Equal(t, "Bearer auth-access-token", exchangeAuth)
	assert.Contains(t, out.String(), "Code: ABCD-EFGH")
	assert.Contains(t, out.String(), ".")
}

func TestLoginWithBrowserFallsBackToVerificationURIWhenCompleteMissing(t *testing.T) {
	cfg := testAuthConfig(t)

	timeoutTimer := newAuthFakeTicker()
	pollTicker := newAuthFakeTicker()
	dotTicker := newAuthFakeTicker()
	var openedURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimSuffix(r.URL.Path, "/") {
		case "/auth/device/authorize":
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"device_code":               "device-code",
				"user_code":                 "ABCD-EFGH",
				"verification_uri":          "https://volcano.dev/device",
				"verification_uri_complete": "",
				"expires_in":                120,
				"interval":                  1,
			})
		case "/auth/device/token":
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"access_token":  "auth-access-token",
				"token_type":    "bearer",
				"expires_in":    3600,
				"refresh_token": "auth-refresh-token",
			})
		case "/auth/platform/exchange":
			writeAuthJSON(t, w, http.StatusOK, map[string]any{
				"token":      "platform-token",
				"user_id":    "platform-user-1",
				"token_id":   "33333333-3333-4333-8333-333333333333",
				"expires_at": time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		OpenBrowser: func(url string) error {
			openedURL = url
			return nil
		},
		NewTimer: func(time.Duration) cliruntime.Timer { return timeoutTimer },
		NewTicker: func(time.Duration) cliruntime.Ticker {
			if pollTicker.created.CompareAndSwap(false, true) {
				return pollTicker
			}
			return dotTicker
		},
	}

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := NewService(deps).LoginWithBrowser(context.Background(), cfg, &out)
		done <- err
	}()

	pollTicker.tick()

	require.NoError(t, <-done, "output:\n%s", out.String())
	assert.Equal(t, "https://volcano.dev/device", openedURL)
	assert.Contains(t, out.String(), "Opening browser: https://volcano.dev/device")
}

func TestLoginWithBrowserFailsAfterConsecutivePollErrors(t *testing.T) {
	cfg := testAuthConfig(t)

	timeoutTimer := newAuthFakeTicker()
	pollTicker := newAuthFakeTicker()
	dotTicker := newAuthFakeTicker()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimSuffix(r.URL.Path, "/") {
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
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("temporary failure"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := cliruntime.Deps{
		HTTPClient:  server.Client(),
		APIBaseURL:  server.URL,
		OpenBrowser: func(string) error { return nil },
		NewTimer:    func(time.Duration) cliruntime.Timer { return timeoutTimer },
		NewTicker: func(time.Duration) cliruntime.Ticker {
			if pollTicker.created.CompareAndSwap(false, true) {
				return pollTicker
			}
			return dotTicker
		},
	}

	var out bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := NewService(deps).LoginWithBrowser(context.Background(), cfg, &out)
		done <- err
	}()

	pollTicker.tick()
	pollTicker.tick()
	pollTicker.tick()

	require.ErrorContains(t, <-done, "failed to poll device token")
}

type authFakeTicker struct {
	ch      chan time.Time
	created atomic.Bool
}

func newAuthFakeTicker() *authFakeTicker {
	return &authFakeTicker{ch: make(chan time.Time, 10)}
}

func (t *authFakeTicker) C() <-chan time.Time {
	return t.ch
}

func (t *authFakeTicker) Reset(time.Duration) {}

func (t *authFakeTicker) Stop() {}

func (t *authFakeTicker) tick() {
	t.ch <- time.Now()
}

func TestResolveDeviceClientIDLocalURL(t *testing.T) {
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "env-device-client")

	for _, apiURL := range []string{
		"http://localhost:8000",
		"http://127.0.0.1:8000",
		"http://[::1]:8000",
		"http://LOCALHOST:8000",
	} {
		t.Run(apiURL, func(t *testing.T) {
			got, err := resolveDeviceClientID(apiURL)
			require.NoError(t, err)
			assert.Equal(t, localmode.DeviceClientID, got)
		})
	}
}

func TestResolveDeviceClientIDCloudEnv(t *testing.T) {
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "env-device-client")

	got, err := resolveDeviceClientID("https://api.volcano.dev")
	require.NoError(t, err)
	assert.Equal(t, "env-device-client", got)
}

func TestResolveDeviceClientIDMissing(t *testing.T) {
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	_, err := resolveDeviceClientID("https://api.volcano.dev")
	require.Error(t, err)
}

func TestIsLocalAPIURL(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"http://localhost:8000", true},
		{"https://localhost", true},
		{"http://127.0.0.1:8000", true},
		{"http://127.7.7.7:8000", true},
		{"http://[::1]:8000", true},
		{"https://api.volcano.dev", false},
		{"https://api.staging.volcano.dev", false},
		{"http://192.168.1.10:8000", false},
		{"", false},
		{"::not a url::", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			assert.Equal(t, tc.want, isLocalAPIURL(tc.url))
		})
	}
}

func testAuthConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_WEB_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
	return config.Default()
}

func writeAuthJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
