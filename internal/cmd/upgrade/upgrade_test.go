package upgrade

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/update"
	"github.com/Kong/volcano-cli/internal/version"
)

func TestUpgradeCommand(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })

	binaryName, err := update.PlatformBinaryName()
	require.NoError(t, err)
	newBinary := []byte("new volcano binary")
	checksum := sha256.Sum256(newBinary)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(checksum[:]), binaryName)

	server := newUpgradeTestServer(t, binaryName, newBinary, checksums)
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("old volcano binary"), 0o755))
	cosignCalled := false
	deps := cliruntime.Deps{
		HTTPClient:         server.Client(),
		UpdateGitHubAPIURL: server.URL,
		ExecutablePath:     exePath,
		UpdateCommandRunner: cliruntime.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			cosignCalled = true
			return nil, nil
		}),
	}

	out, err := executeUpgradeCommand(t, New(deps))
	require.NoError(t, err)
	assert.Contains(t, out, "Upgraded Volcano CLI from v1.2.3 to v1.2.4")
	installed, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, newBinary, installed)
	assert.False(t, cosignCalled)
}

func TestUpgradeCommandVerifySignatureFlag(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })

	binaryName, err := update.PlatformBinaryName()
	require.NoError(t, err)
	newBinary := []byte("new volcano binary")
	checksum := sha256.Sum256(newBinary)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(checksum[:]), binaryName)

	server := newUpgradeTestServer(t, binaryName, newBinary, checksums)
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("old volcano binary"), 0o755))
	cosignCalled := false
	deps := cliruntime.Deps{
		HTTPClient:         server.Client(),
		UpdateGitHubAPIURL: server.URL,
		ExecutablePath:     exePath,
		UpdateCommandRunner: cliruntime.CommandRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			cosignCalled = true
			return nil, nil
		}),
	}

	_, err = executeUpgradeCommand(t, New(deps), "--verify-signature")
	require.NoError(t, err)
	assert.True(t, cosignCalled)
}

func TestUpgradeCommandRejectsArgs(t *testing.T) {
	cmd := New(cliruntime.Deps{})
	_, err := executeUpgradeCommand(t, cmd, "v1.2.3")
	require.Error(t, err)
	assert.ErrorContains(t, err, `unknown command "v1.2.3"`)
}

// withInstructions drives api.LastInstructions() through a real api.Client
// call against a test server, exercising PrintAPIInstructionNotices exactly
// as production does (via observed response headers), with no reliance on
// api's unexported state.
func withInstructions(t *testing.T, cliInstruction, latest, deviceInstruction string) {
	t.Helper()
	// recordInstructions is sticky (VOL-180): a field a response omits doesn't
	// clear a value recorded by an earlier test. Reset explicitly so each test
	// starts from the zero value regardless of execution order.
	api.ResetLastInstructionsForTest()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if cliInstruction != "" {
			w.Header().Set("X-Volcano-CLI-Instruction", cliInstruction)
		}
		if latest != "" {
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

func TestPrintAPIInstructionNotices_Suggestion(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })
	withInstructions(t, api.CLIInstructionSuggestionVersionUpgrade, "v1.5.0", "")

	cmd := &cobra.Command{Use: "projects"}
	var out bytes.Buffer
	cmd.SetErr(&out)
	PrintAPIInstructionNotices(cmd, cliruntime.Deps{})

	assert.Contains(t, out.String(), "A newer Volcano CLI version is available: v1.5.0 (current v1.2.3). Run `volcano upgrade` to upgrade.")
}

func TestPrintAPIInstructionNotices_SuggestionWithoutLatestVersion(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })
	withInstructions(t, api.CLIInstructionSuggestionVersionUpgrade, "", "")

	cmd := &cobra.Command{Use: "projects"}
	var out bytes.Buffer
	cmd.SetErr(&out)
	PrintAPIInstructionNotices(cmd, cliruntime.Deps{})

	assert.Contains(t, out.String(), "A newer Volcano CLI version is available (current v1.2.3). Run `volcano upgrade` to upgrade.")
}

func TestPrintAPIInstructionNotices_Deprecation(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v0.9.0"
	t.Cleanup(func() { version.Version = oldVersion })
	withInstructions(t, api.CLIInstructionRequireVersionUpgrade, "v1.5.0", "")

	cmd := &cobra.Command{Use: "login"}
	var out bytes.Buffer
	cmd.SetErr(&out)
	PrintAPIInstructionNotices(cmd, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Volcano CLI v0.9.0 is no longer supported. Upgrade to v1.5.0 or later:\n  volcano upgrade")
}

func TestPrintAPIInstructionNotices_NotEnoughCredit(t *testing.T) {
	// Reserved instruction (VOL-180 PR review discussion): the API never
	// emits this yet, but the CLI-side handling is real and testable so a
	// future billing-service integration needs no CLI change to start working.
	withInstructions(t, api.CLIInstructionNotEnoughCredit, "", "")

	cmd := &cobra.Command{Use: "functions"}
	var out bytes.Buffer
	cmd.SetErr(&out)
	PrintAPIInstructionNotices(cmd, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Your project does not have enough credit to complete this request.")
}

func TestPrintAPIInstructionNotices_LowCreditWarning(t *testing.T) {
	withInstructions(t, api.CLIInstructionLowCreditWarning, "", "")

	cmd := &cobra.Command{Use: "functions"}
	var out bytes.Buffer
	cmd.SetErr(&out)
	PrintAPIInstructionNotices(cmd, cliruntime.Deps{})

	assert.Contains(t, out.String(), "Your project is running low on credit.")
}

func TestPrintAPIInstructionNotices_UsesCommandPathPrefix(t *testing.T) {
	withInstructions(t, api.CLIInstructionSuggestionVersionUpgrade, "v1.5.0", "")

	cmd := &cobra.Command{Use: "projects"}
	var out bytes.Buffer
	cmd.SetErr(&out)
	PrintAPIInstructionNotices(cmd, cliruntime.Deps{CommandPathPrefix: "acme"})

	assert.Contains(t, out.String(), "Run `acme upgrade` to upgrade.")
}

func executeUpgradeCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func newUpgradeTestServer(t *testing.T, binaryName string, binary []byte, checksums string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			writeUpgradeJSON(t, w, update.Release{TagName: "v1.2.4", Assets: []update.Asset{
				{Name: binaryName, BrowserDownloadURL: "http://" + r.Host + "/assets/" + binaryName},
				{Name: binaryName + ".sigstore.json", BrowserDownloadURL: "http://" + r.Host + "/assets/" + binaryName + ".sigstore.json"},
				{Name: "SHA256SUMS", BrowserDownloadURL: "http://" + r.Host + "/assets/SHA256SUMS"},
			}})
		case "/assets/" + binaryName:
			_, _ = w.Write(binary)
		case "/assets/" + binaryName + ".sigstore.json":
			_, _ = w.Write([]byte(`{"bundle":true}`))
		case "/assets/SHA256SUMS":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
}

func writeUpgradeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
