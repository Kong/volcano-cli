package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	rootcmd "github.com/Kong/volcano-cli/internal/cmd/root"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// withInstructions drives api.LastInstructions() through a real api.Client
// call against a test server, mirroring how production populates it from
// response headers (VOL-180).
func withInstructions(t *testing.T, latest, deviceInstruction string) {
	t.Helper()
	// recordInstructions is sticky (VOL-180): a field a response omits doesn't
	// clear a value recorded by an earlier test. Reset explicitly so each test
	// starts from the zero value regardless of execution order.
	api.ResetLastInstructionsForTest()
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
	api.ResetLastInstructionsForTest()
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
	api.ResetLastInstructionsForTest()
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

func TestRun_SuccessWithDeprecationNoticeOnExemptRoute(t *testing.T) {
	// The exact scenario the notice-printing path exists for (per
	// printDeprecationWarning's doc comment): a deprecated CLI's request lands
	// on an exempt route (e.g. `login`) and succeeds — the server sets the
	// require_version_upgrade header but doesn't 426 it. The user still needs
	// to learn their CLI is deprecated even though this command was let
	// through. Only the deprecation-error (426) path had run()-level coverage
	// before this; this is the success-path counterpart.
	api.ResetLastInstructionsForTest()
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

func TestRun_DeprecationErrorShortCircuitsWithoutDuplicateNotice(t *testing.T) {
	api.ResetLastInstructionsForTest()
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
	api.ResetLastInstructionsForTest()
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
	api.ResetLastInstructionsForTest()
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
