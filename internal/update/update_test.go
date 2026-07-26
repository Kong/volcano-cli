package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectInstallMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want InstallMethod
	}{
		{name: "npm global", path: "/usr/local/lib/node_modules/@volcano.dev/cli/bin/volcano-macos-arm64", want: InstallNPM},
		{name: "pnpm global", path: "/home/u/Library/pnpm/global/5/node_modules/@volcano.dev/cli/bin/volcano-linux-amd64", want: InstallPNPM},
		{name: "bun global", path: "/home/u/.bun/install/global/node_modules/@volcano.dev/cli/bin/volcano-linux-amd64", want: InstallBun},
		{name: "homebrew", path: "/opt/homebrew/Cellar/volcano/0.2.1/bin/volcano", want: InstallBrew},
		{name: "script install", path: "/usr/local/bin/volcano", want: InstallScript},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DetectInstallMethod(tt.path))
		})
	}
}

func TestDetectInstallMethodMarkerOverridesPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// A binary path that looks like an npm install, but the marker says pnpm.
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(filepath.Join(dir, installMarkerName), []byte("pnpm\n"), 0o644))
	assert.Equal(t, InstallPNPM, DetectInstallMethod(exePath))
}

func TestUpgradeDelegatesToPackageManager(t *testing.T) {
	t.Parallel()

	// Latest release is newer than current, so delegation must happen.
	server := newLatestReleaseServer(t, "v1.3.0")
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("binary"), 0o755))

	var gotName string
	var gotArgs []string
	var out bytes.Buffer
	err := Upgrade(context.Background(), "v1.2.3", &out, Options{
		InstallMethod:  InstallNPM,
		ExecutablePath: exePath,
		GitHubAPIURL:   server.URL,
		HTTPClient:     server.Client(),
		LookPath:       func(string) (string, error) { return "/usr/bin/npm", nil },
		ManagerRunner: func(_ context.Context, _ io.Writer, name string, args ...string) error {
			gotName, gotArgs = name, args
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "npm", gotName)
	assert.Equal(t, []string{"install", "-g", "@volcano.dev/cli@latest"}, gotArgs)
	// The binary must be left untouched (no self-replace).
	installed, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "binary", string(installed))
}

func TestUpgradeManagerSkipsWhenUpToDate(t *testing.T) {
	t.Parallel()

	// Latest release equals current: the package manager must not be invoked.
	server := newLatestReleaseServer(t, "v1.2.3")
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("binary"), 0o755))

	ran := false
	var out bytes.Buffer
	err := Upgrade(context.Background(), "v1.2.3", &out, Options{
		InstallMethod:  InstallNPM,
		ExecutablePath: exePath,
		GitHubAPIURL:   server.URL,
		HTTPClient:     server.Client(),
		LookPath:       func(string) (string, error) { return "/usr/bin/npm", nil },
		ManagerRunner: func(context.Context, io.Writer, string, ...string) error {
			ran = true
			return nil
		},
	})
	require.NoError(t, err)
	assert.False(t, ran)
	assert.Contains(t, out.String(), "already up to date (v1.2.3)")
}

func TestUpgradePrintsCommandWhenManagerMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("binary"), 0o755))

	// Newer release available so the up-to-date check falls through to the
	// lookPath miss, which prints the command instead of running it.
	server := newLatestReleaseServer(t, "v1.3.0")
	defer server.Close()

	ran := false
	var out bytes.Buffer
	err := Upgrade(context.Background(), "v1.2.3", &out, Options{
		InstallMethod:  InstallBrew,
		ExecutablePath: exePath,
		GitHubAPIURL:   server.URL,
		HTTPClient:     server.Client(),
		LookPath:       func(string) (string, error) { return "", exec.ErrNotFound },
		ManagerRunner: func(context.Context, io.Writer, string, ...string) error {
			ran = true
			return nil
		},
	})
	require.NoError(t, err)
	assert.False(t, ran)
	assert.Contains(t, out.String(), "brew upgrade volcano")
}

// newLatestReleaseServer serves a minimal GitHub releases/latest response with
// the given tag, for exercising the manager path's up-to-date check.
func newLatestReleaseServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases/latest" {
			writeUpdateJSON(t, w, Release{TagName: tag})
			return
		}
		http.NotFound(w, r)
	}))
}

func TestNewerThan(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		candidate string
		current   string
		want      bool
	}{
		{name: "patch", candidate: "v1.2.4", current: "v1.2.3", want: true},
		{name: "minor", candidate: "v1.3.0", current: "1.2.9", want: true},
		{name: "major", candidate: "v2.0.0", current: "v1.9.9", want: true},
		{name: "same", candidate: "v1.2.3", current: "v1.2.3", want: false},
		{name: "older", candidate: "v1.2.2", current: "v1.2.3", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewerThan(tt.candidate, tt.current)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewerThanRejectsPrerelease(t *testing.T) {
	t.Parallel()

	_, err := NewerThan("v1.2.3-beta.1", "v1.2.2")
	require.Error(t, err)
}

func TestDefaultAssetDownloadClientAvoidsTotalBodyTimeout(t *testing.T) {
	t.Parallel()

	releaseClient, ok := releaseHTTPClient(Options{}).(*http.Client)
	require.True(t, ok)
	assert.Equal(t, defaultTimeout, releaseClient.Timeout)

	downloadClient, ok := assetDownloadHTTPClient(Options{}).(*http.Client)
	require.True(t, ok)
	assert.Zero(t, downloadClient.Timeout)
	transport, ok := downloadClient.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, defaultTimeout, transport.ResponseHeaderTimeout)
}

func TestUpgradeDownloadsChecksumsAndReplacesExecutable(t *testing.T) {
	t.Parallel()

	binaryName, err := PlatformBinaryName()
	require.NoError(t, err)
	newBinary := []byte("new volcano binary")
	checksum := sha256.Sum256(newBinary)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(checksum[:]), binaryName)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			writeUpdateJSON(t, w, Release{TagName: "v1.2.4", Assets: []Asset{
				{Name: binaryName, BrowserDownloadURL: "http://" + r.Host + "/assets/" + binaryName},
				{Name: binaryName + ".sigstore.json", BrowserDownloadURL: "http://" + r.Host + "/assets/" + binaryName + ".sigstore.json"},
				{Name: "SHA256SUMS", BrowserDownloadURL: "http://" + r.Host + "/assets/SHA256SUMS"},
			}})
		case "/assets/" + binaryName:
			_, _ = w.Write(newBinary)
		case "/assets/" + binaryName + ".sigstore.json":
			_, _ = w.Write([]byte(`{"bundle":true}`))
		case "/assets/SHA256SUMS":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("old volcano binary"), 0o755))
	cosignCalled := false

	var out bytes.Buffer
	err = Upgrade(context.Background(), "v1.2.3", &out, Options{
		GitHubAPIURL:   server.URL,
		HTTPClient:     server.Client(),
		ExecutablePath: exePath,
		CommandRunner: RunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			cosignCalled = true
			return nil, nil
		}),
	})
	require.NoError(t, err)

	installed, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, newBinary, installed)
	assert.Contains(t, out.String(), "Upgraded Volcano CLI from v1.2.3 to v1.2.4")
	assert.False(t, cosignCalled)
}

func TestUpgradeVerifiesSignatureWhenRequired(t *testing.T) {
	t.Parallel()

	binaryName, err := PlatformBinaryName()
	require.NoError(t, err)
	newBinary := []byte("new volcano binary")
	checksum := sha256.Sum256(newBinary)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(checksum[:]), binaryName)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			writeUpdateJSON(t, w, Release{TagName: "v1.2.4", Assets: []Asset{
				{Name: binaryName, BrowserDownloadURL: "http://" + r.Host + "/assets/" + binaryName},
				{Name: binaryName + ".sigstore.json", BrowserDownloadURL: "http://" + r.Host + "/assets/" + binaryName + ".sigstore.json"},
				{Name: "SHA256SUMS", BrowserDownloadURL: "http://" + r.Host + "/assets/SHA256SUMS"},
			}})
		case "/assets/" + binaryName:
			_, _ = w.Write(newBinary)
		case "/assets/" + binaryName + ".sigstore.json":
			_, _ = w.Write([]byte(`{"bundle":true}`))
		case "/assets/SHA256SUMS":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("old volcano binary"), 0o755))
	var cosignArgs []string
	runner := RunnerFunc(func(_ context.Context, name string, args ...string) ([]byte, error) {
		cosignArgs = append([]string{name}, args...)
		return nil, nil
	})

	err = Upgrade(context.Background(), "v1.2.3", io.Discard, Options{
		GitHubAPIURL:                 server.URL,
		HTTPClient:                   server.Client(),
		ExecutablePath:               exePath,
		CommandRunner:                runner,
		RequireSignatureVerification: true,
	})
	require.NoError(t, err)

	assert.Equal(t, "cosign", cosignArgs[0])
	assert.Contains(t, cosignArgs, "verify-blob")
	assert.Contains(t, cosignArgs, "--certificate-identity")
	assert.Contains(t, cosignArgs, signatureWorkflow+"@refs/tags/v1.2.4")
}

func TestUpgradeRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()

	binaryName, err := PlatformBinaryName()
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			writeUpdateJSON(t, w, Release{TagName: "v1.2.4", Assets: []Asset{
				{Name: binaryName, BrowserDownloadURL: "http://" + r.Host + "/assets/" + binaryName},
				{Name: binaryName + ".sigstore.json", BrowserDownloadURL: "http://" + r.Host + "/assets/" + binaryName + ".sigstore.json"},
				{Name: "SHA256SUMS", BrowserDownloadURL: "http://" + r.Host + "/assets/SHA256SUMS"},
			}})
		case "/assets/" + binaryName:
			_, _ = w.Write([]byte("new volcano binary"))
		case "/assets/" + binaryName + ".sigstore.json":
			_, _ = w.Write([]byte(`{"bundle":true}`))
		case "/assets/SHA256SUMS":
			_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  " + binaryName + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("old volcano binary"), 0o755))

	err = Upgrade(context.Background(), "v1.2.3", io.Discard, Options{
		GitHubAPIURL:   server.URL,
		HTTPClient:     server.Client(),
		ExecutablePath: exePath,
		CommandRunner:  RunnerFunc(func(context.Context, string, ...string) ([]byte, error) { return nil, nil }),
	})
	require.ErrorContains(t, err, "checksum mismatch")

	installed, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "old volcano binary", string(installed))
}

func TestUpgradeReportsCosignNotInstalled(t *testing.T) {
	t.Parallel()

	binaryName, err := PlatformBinaryName()
	require.NoError(t, err)
	newBinary := []byte("new volcano binary")
	checksum := sha256.Sum256(newBinary)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(checksum[:]), binaryName)

	server := newReleaseServer(t, binaryName, newBinary, checksums)
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("old volcano binary"), 0o755))

	err = Upgrade(context.Background(), "v1.2.3", io.Discard, Options{
		GitHubAPIURL:                 server.URL,
		HTTPClient:                   server.Client(),
		ExecutablePath:               exePath,
		RequireSignatureVerification: true,
		CommandRunner: RunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			return nil, exec.ErrNotFound
		}),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "verify Volcano CLI signature with cosign")
	require.ErrorIs(t, err, exec.ErrNotFound)

	installed, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "old volcano binary", string(installed))
}

func TestUpgradeReportsCosignVerificationFailure(t *testing.T) {
	t.Parallel()

	binaryName, err := PlatformBinaryName()
	require.NoError(t, err)
	newBinary := []byte("new volcano binary")
	checksum := sha256.Sum256(newBinary)
	checksums := fmt.Sprintf("%s  %s\n", hex.EncodeToString(checksum[:]), binaryName)

	server := newReleaseServer(t, binaryName, newBinary, checksums)
	defer server.Close()

	dir := t.TempDir()
	exePath := filepath.Join(dir, "volcano")
	require.NoError(t, os.WriteFile(exePath, []byte("old volcano binary"), 0o755))

	err = Upgrade(context.Background(), "v1.2.3", io.Discard, Options{
		GitHubAPIURL:                 server.URL,
		HTTPClient:                   server.Client(),
		ExecutablePath:               exePath,
		RequireSignatureVerification: true,
		CommandRunner: RunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			return []byte("certificate identity mismatch"), errors.New("exit status 1")
		}),
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "certificate identity mismatch")

	installed, err := os.ReadFile(exePath)
	require.NoError(t, err)
	assert.Equal(t, "old volcano binary", string(installed))
}

func newReleaseServer(t *testing.T, binaryName string, binary []byte, checksums string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases/latest":
			writeUpdateJSON(t, w, Release{TagName: "v1.2.4", Assets: []Asset{
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

func writeUpdateJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
