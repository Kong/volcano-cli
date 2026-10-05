package update

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type downloadTransport func(*http.Request) (*http.Response, error)

func (f downloadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDefaultReleaseCheckUsesVolcano(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: downloadTransport(func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, "https://download.volcano.dev/builds/releases/latest-version", r.URL.String())
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("v1.2.4\n")), Header: make(http.Header)}, nil
	})}
	release, err := LatestRelease(t.Context(), Options{HTTPClient: client})
	require.NoError(t, err)
	assert.Equal(t, "https://download.volcano.dev/builds/releases/download/v1.2.4/SHA256SUMS", release.AssetURL("SHA256SUMS"))
}

func TestDownloadReleaseRejectsInvalidPointer(t *testing.T) {
	t.Parallel()
	for _, body := range []string{"", "../bad", "v01.2.3", "v1.2.3\nv1.2.4", "1.2.3", strings.Repeat("1", 128)} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
			defer server.Close()
			_, err := LatestRelease(t.Context(), Options{DownloadURL: server.URL})
			require.Error(t, err)
		})
	}
}

func TestDownloadUpgradePinsVersionAndPreservesBinaryOnFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"", "checksum", "signature", "missing", "nochecksums"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			name, err := PlatformBinaryName()
			require.NoError(t, err)
			binary := []byte("new binary")
			hash := sha256.Sum256(binary)
			var pointerReads, binaryReads int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/latest-version":
					pointerReads++
					_, _ = io.WriteString(w, "v1.2.4\n")
				case "/download/v1.2.4/" + name:
					binaryReads++
					if failure == "missing" {
						http.NotFound(w, r)
						return
					}
					_, _ = w.Write(binary)
				case "/download/v1.2.4/" + name + ".sigstore.json":
					_, _ = io.WriteString(w, "bundle")
				case "/download/v1.2.4/SHA256SUMS":
					if failure == "nochecksums" {
						http.NotFound(w, r)
						return
					}
					if failure == "checksum" {
						hash = sha256.Sum256([]byte("wrong"))
					}
					_, _ = fmt.Fprintf(w, "%x  %s\n", hash, name)
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			exe := filepath.Join(t.TempDir(), "volcano")
			require.NoError(t, os.WriteFile(exe, []byte("old binary"), 0o755))
			err = Upgrade(t.Context(), "v1.2.3", io.Discard, Options{
				DownloadURL: server.URL, ExecutablePath: exe, RequireSignatureVerification: true,
				CommandRunner: RunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
					assert.Contains(t, args, signatureWorkflow+"@refs/tags/v1.2.4")
					if failure == "signature" {
						return nil, errors.New("invalid signature")
					}
					return nil, nil
				}),
			})
			if failure == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			content, readErr := os.ReadFile(exe)
			require.NoError(t, readErr)
			if failure == "" {
				assert.Equal(t, binary, content)
			} else {
				assert.Equal(t, "old binary", string(content))
			}
			assert.Equal(t, 1, pointerReads)
			if failure == "nochecksums" {
				assert.Zero(t, binaryReads, "a missing SHA256SUMS must fail before the binary download")
			}
		})
	}
}
