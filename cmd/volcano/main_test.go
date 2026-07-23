package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	rootcmd "github.com/Kong/volcano-cli/internal/cmd/root"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func resetInstructions(t *testing.T) {
	t.Helper()
	api.ResetLastInstructionsForTest()
	t.Cleanup(api.ResetLastInstructionsForTest)
}

// withInstructions drives api.LastInstructions() through a real api.Client
// call against a test server, mirroring how production populates it from
// response headers (VOL-180).
func withInstructions(t *testing.T, latest, deviceInstruction string) {
	t.Helper()
	// recordInstructions is sticky (VOL-180), so isolate both the beginning and
	// end of each test from the process-global state.
	resetInstructions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if latest != "" {
			// The server never sends X-Volcano-CLI-Latest-Version without a
			// paired instruction (setLatestVersionHeader is only called from the
			// suggest/deprecate branches) — recordInstructions relies on that
			// contract, so this helper must too.
			w.Header().Set("X-Volcano-CLI-Instruction", api.CLIInstructionRequireVersionUpgrade)
			w.Header().Set("X-Volcano-CLI-Latest-Version", latest)
		}
		if deviceInstruction != "" {
			w.Header().Set("X-Volcano-Device-Instruction", deviceInstruction)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`))
	}))
	t.Cleanup(server.Close)

	client, err := api.NewClient(server.URL, "", api.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	_, err = client.ListProjects(context.Background(), api.DefaultPage, api.DefaultLimit)
	require.NoError(t, err)
}

func TestPrintDeprecationError_WithLatestVersion(t *testing.T) {
	withInstructions(t, "v1.5.0", "")
	var out bytes.Buffer

	printDeprecationError(&out, &api.Error{StatusCode: http.StatusUpgradeRequired, Message: "cli version no longer supported; run `volcano upgrade`"}, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Error: HTTP 426: cli version no longer supported; run `volcano upgrade`")
	assert.Contains(t, out.String(), "Upgrade to v1.5.0: volcano upgrade")
}

func TestPrintDeprecationError_WithoutLatestVersion(t *testing.T) {
	withInstructions(t, "", "")
	var out bytes.Buffer

	printDeprecationError(&out, &api.Error{StatusCode: http.StatusUpgradeRequired, Message: "cli version no longer supported"}, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Error: HTTP 426: cli version no longer supported")
	assert.NotContains(t, out.String(), "Upgrade to")
}

func TestPrintDeprecationError_UsesCommandPathPrefix(t *testing.T) {
	withInstructions(t, "v1.5.0", "")
	var out bytes.Buffer

	printDeprecationError(&out, &api.Error{StatusCode: http.StatusUpgradeRequired}, cliruntime.Deps{CommandPathPrefix: "acme"})

	assert.Contains(t, out.String(), "Upgrade to v1.5.0: acme upgrade")
}

func TestPrintError_ReauthHint(t *testing.T) {
	withInstructions(t, "", "reauth")
	var out bytes.Buffer

	printError(&out, &api.Error{StatusCode: http.StatusUnauthorized, Message: "token expired"}, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Error: HTTP 401: token expired")
	assert.Contains(t, out.String(), "Run `volcano login` to re-authenticate.")
}

func TestPrintError_NoReauthHintWithoutSignal(t *testing.T) {
	withInstructions(t, "", "")
	var out bytes.Buffer

	printError(&out, &api.Error{StatusCode: http.StatusInternalServerError, Message: "boom"}, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Error: HTTP 500: boom")
	assert.NotContains(t, out.String(), "re-authenticate")
}

func TestPrintError_ReauthHintUsesCommandPathPrefix(t *testing.T) {
	withInstructions(t, "", "reauth")
	var out bytes.Buffer

	printError(&out, &api.Error{StatusCode: http.StatusUnauthorized}, cliruntime.Deps{CommandPathPrefix: "acme"})

	assert.Contains(t, out.String(), "Run `acme login` to re-authenticate.")
}

// runDeps builds cliruntime.Deps + a rootcmd.New(deps) wired to server via an
// in-memory config (no disk/env), so tests can drive run() through a real
// command instead of poking internal state.
func runDeps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{
		HTTPClient: server.Client(),
		ConfigLoader: func() (*cliconfig.Config, error) {
			return &cliconfig.Config{
				APIBaseURL: server.URL,
				UserToken:  "test-token",
				IgnoreEnv:  true,
			}, nil
		},
	}
}

func TestRun_SuccessNoNotice(t *testing.T) {
	resetInstructions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`))
	}))
	defer server.Close()

	deps := runDeps(server)
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"projects", "list"})

	code := run(root, deps)

	assert.Equal(t, 0, code)
	assert.Empty(t, stderr.String(), "no notice pending, stderr must stay empty")
}

func TestRun_SuccessWithSuggestionNotice(t *testing.T) {
	resetInstructions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Volcano-CLI-Instruction", api.CLIInstructionSuggestionVersionUpgrade)
		w.Header().Set("X-Volcano-CLI-Latest-Version", "v1.5.0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`))
	}))
	defer server.Close()

	deps := runDeps(server)
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"projects", "list"})

	code := run(root, deps)

	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "A newer Volcano CLI version is available: v1.5.0")
}

func TestRun_SuccessWithDeprecationNotice(t *testing.T) {
	// A successful API response can still carry a deprecation instruction. The
	// command must warn without changing its successful exit status.
	resetInstructions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Volcano-CLI-Instruction", api.CLIInstructionRequireVersionUpgrade)
		w.Header().Set("X-Volcano-CLI-Latest-Version", "v1.5.0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[],"has_more":false,"page":1,"limit":100,"total":0}`))
	}))
	defer server.Close()

	deps := runDeps(server)
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"projects", "list"})

	code := run(root, deps)

	// The command succeeded (no error to short-circuit on), so the exit code
	// must be 0 even though the CLI is deprecated — only the 426 path (a
	// non-exempt route) exits 1.
	assert.Equal(t, 0, code)
	assert.Contains(t, stderr.String(), "Volcano CLI")
	assert.Contains(t, stderr.String(), "is no longer supported. Upgrade to v1.5.0 or later:")
}

func TestRun_SuccessWithDeprecationNoticeOnExemptAuthRoute(t *testing.T) {
	resetInstructions(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")

	pollTicker := newMainTestTicker()
	dotTicker := newMainTestTicker()
	timeoutTimer := newMainTestTicker()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Volcano-CLI-Instruction", api.CLIInstructionRequireVersionUpgrade)
		w.Header().Set("X-Volcano-CLI-Latest-Version", "v1.5.0")
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/device/authorize":
			_, _ = w.Write([]byte(`{"device_code":"device-code","user_code":"ABCD-EFGH","verification_uri":"https://volcano.dev/device","verification_uri_complete":"https://volcano.dev/device?user_code=ABCD-EFGH","expires_in":120,"interval":1}`))
		case "/auth/device/token":
			_, _ = w.Write([]byte(`{"access_token":"auth-access-token"}`))
		case "/auth/platform/exchange":
			_, _ = w.Write([]byte(`{"token":"platform-token","user_id":"platform-user-1","token_id":"33333333-3333-4333-8333-333333333333","expires_at":"2030-01-01T00:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var tickerCalls int
	deps := cliruntime.Deps{
		HTTPClient:  server.Client(),
		APIBaseURL:  server.URL,
		OpenBrowser: func(string) error { return nil },
		NewTimer:    func(time.Duration) cliruntime.Timer { return timeoutTimer },
		NewTicker: func(time.Duration) cliruntime.Ticker {
			tickerCalls++
			if tickerCalls == 1 {
				return pollTicker
			}
			return dotTicker
		},
	}
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"login"})

	done := make(chan int, 1)
	go func() { done <- run(root, deps) }()
	pollTicker.tick()
	select {
	case code := <-done:
		assert.Equal(t, 0, code)
	case <-time.After(2 * time.Second):
		t.Fatal("login did not complete")
	}
	assert.Contains(t, stderr.String(), "Volcano CLI")
	assert.Contains(t, stderr.String(), "is no longer supported. Upgrade to v1.5.0 or later:")
	assert.Equal(t, 1, strings.Count(stderr.String(), "Volcano CLI"), "the early auth-route notice must not be repeated after Execute returns")
}

func TestRun_DeprecationErrorShortCircuitsWithoutDuplicateNotice(t *testing.T) {
	resetInstructions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Volcano-CLI-Instruction", api.CLIInstructionRequireVersionUpgrade)
		w.Header().Set("X-Volcano-CLI-Latest-Version", "v1.5.0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUpgradeRequired)
		_, _ = w.Write([]byte(`{"error":"cli version no longer supported; run ` + "`volcano upgrade`" + `"}`))
	}))
	defer server.Close()

	deps := runDeps(server)
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"projects", "list"})

	code := run(root, deps)

	assert.Equal(t, 1, code)
	text := stderr.String()
	assert.Contains(t, text, "Error:")
	assert.Contains(t, text, "Upgrade to v1.5.0: volcano upgrade")
	// The 426 path must not ALSO print the generic non-blocking suggestion
	// notice (a distinct phrase from the deprecation error's own message) —
	// that would tell the user the same thing twice in different words.
	assert.NotContains(t, text, "A newer Volcano CLI version is available", "deprecation error path must not duplicate the suggestion notice: %q", text)
}

func TestRun_NonBlockingErrorPrintsErrorBeforeNotice(t *testing.T) {
	// A command that fails for an unrelated reason (404 here) while also
	// carrying a pending suggestion notice must print the actual error first.
	resetInstructions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Volcano-CLI-Instruction", api.CLIInstructionSuggestionVersionUpgrade)
		w.Header().Set("X-Volcano-CLI-Latest-Version", "v1.5.0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"project not found"}`))
	}))
	defer server.Close()

	deps := runDeps(server)
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"projects", "get", "11111111-1111-1111-1111-111111111111"})

	code := run(root, deps)

	assert.Equal(t, 1, code)
	text := stderr.String()
	require.Contains(t, text, "Error:")
	require.Contains(t, text, "newer Volcano CLI version is available")
	assert.Less(t, strings.Index(text, "Error:"), strings.Index(text, "newer Volcano CLI version is available"),
		"the error line must come before the notice: %q", text)
}

func TestRun_NonBlockingErrorWithReauthHint(t *testing.T) {
	resetInstructions(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Volcano-Device-Instruction", api.DeviceInstructionReauth)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token expired"}`))
	}))
	defer server.Close()

	deps := runDeps(server)
	root := rootcmd.New(deps)
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"projects", "list"})

	code := run(root, deps)

	assert.Equal(t, 1, code)
	text := stderr.String()
	require.Contains(t, text, "Error:")
	require.Contains(t, text, "Run `volcano login` to re-authenticate.")
	assert.Less(t, strings.Index(text, "Error:"), strings.Index(text, "Run `volcano login`"),
		"the error line must come before the reauth hint: %q", text)
}

type mainTestTicker struct {
	ch chan time.Time
}

func newMainTestTicker() *mainTestTicker {
	return &mainTestTicker{ch: make(chan time.Time, 1)}
}

func (t *mainTestTicker) C() <-chan time.Time { return t.ch }
func (t *mainTestTicker) Stop()               {}
func (t *mainTestTicker) Reset(time.Duration) {}
func (t *mainTestTicker) tick()               { t.ch <- time.Now() }
