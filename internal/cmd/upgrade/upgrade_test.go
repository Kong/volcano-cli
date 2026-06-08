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

func TestUpdateNoticePrintsForOutdatedVersion(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/releases/latest", r.URL.Path)
		writeUpgradeJSON(t, w, update.Release{TagName: "v1.2.4"})
	}))
	defer server.Close()

	cmd := noticeTestCommand("init")
	var out bytes.Buffer
	cmd.SetErr(&out)
	MaybePrintUpdateNotice(cmd, cliruntime.Deps{
		HTTPClient:         server.Client(),
		UpdateGitHubAPIURL: server.URL,
	})
	assert.Contains(t, out.String(), "A newer Volcano CLI version is available: v1.2.4 (current v1.2.3). Run `volcano upgrade` to upgrade.")
}

func TestUpdateNoticeSkipsVersionCommand(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeUpgradeJSON(t, w, update.Release{TagName: "v1.2.4"})
	}))
	defer server.Close()

	cmd := noticeTestCommand("version")
	var out bytes.Buffer
	cmd.SetErr(&out)
	MaybePrintUpdateNotice(cmd, cliruntime.Deps{
		HTTPClient:         server.Client(),
		UpdateGitHubAPIURL: server.URL,
	})
	assert.Empty(t, out.String())
	assert.Zero(t, requests)
}

func TestUpdateNoticeSkipsImplicitRootHelp(t *testing.T) {
	oldVersion := version.Version
	version.Version = "v1.2.3"
	t.Cleanup(func() { version.Version = oldVersion })

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeUpgradeJSON(t, w, update.Release{TagName: "v1.2.4"})
	}))
	defer server.Close()

	cmd := &cobra.Command{Use: "volcano"}
	var out bytes.Buffer
	cmd.SetErr(&out)
	MaybePrintUpdateNotice(cmd, cliruntime.Deps{
		HTTPClient:         server.Client(),
		UpdateGitHubAPIURL: server.URL,
	})
	assert.Empty(t, out.String())
	assert.Zero(t, requests)
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

func noticeTestCommand(name string) *cobra.Command {
	root := &cobra.Command{Use: "volcano"}
	cmd := &cobra.Command{Use: name}
	root.AddCommand(cmd)
	return cmd
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
