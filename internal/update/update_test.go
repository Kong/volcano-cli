package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestCheckLatest(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/releases/latest", r.URL.Path)
		writeUpdateJSON(t, w, Release{TagName: "v1.2.4"})
	}))
	defer server.Close()

	notice, err := CheckLatest(context.Background(), "v1.2.3", Options{GitHubAPIURL: server.URL, HTTPClient: server.Client()})
	require.NoError(t, err)
	require.NotNil(t, notice)
	assert.Equal(t, "v1.2.3", notice.Current)
	assert.Equal(t, "v1.2.4", notice.Latest)
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

func writeUpdateJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
