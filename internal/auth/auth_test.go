package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/localmode"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const authAlphaProjectID = "11111111-1111-4111-8111-111111111111"

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

	credentials, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).LoginWithToken(context.Background(), cfg, "  valid-token\n")
	require.NoError(t, err)
	assert.Equal(t, "Bearer valid-token", sawAuth)
	assert.Equal(t, Credentials{Token: "valid-token"}, credentials)
}

func TestLoginWithTokenInvalid(t *testing.T) {
	cfg := testAuthConfig(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}))
	defer server.Close()

	_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).LoginWithToken(context.Background(), cfg, "bad-token")
	require.ErrorContains(t, err, "invalid token")
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
	assert.Equal(t, Credentials{Token: "platform-token", UserID: "platform-user-1"}, result.credentials)
	assert.Equal(t, "https://volcano.dev/device?user_code=ABCD-EFGH", openedURL)
	assert.Equal(t, "Bearer auth-access-token", exchangeAuth)
	assert.Contains(t, out.String(), "Code: ABCD-EFGH")
	assert.Contains(t, out.String(), ".")
}

func TestLoginWithBrowserFallsBackToVerificationURI(t *testing.T) {
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
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
	return config.Default()
}

func writeAuthJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
