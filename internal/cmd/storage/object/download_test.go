package object

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDownloadToStdout(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/storage/uploads/") {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("download body"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"download", "uploads", "greetings/hello.txt", "-",
	)
	require.NoError(t, err)
	assert.Equal(t, "download body", out)
}

func TestDownloadToFile(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/storage/uploads/") {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("download body"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"download", "uploads", "greetings/hello.txt", target,
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Downloaded")

	written, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "download body", string(written))

	_, err = os.Stat(target + ".part")
	require.True(t, os.IsNotExist(err), "expected .part file to be removed after successful download, got err=%v", err)
}

func TestDownloadFailurePreservesExistingFile(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "out.txt")
	require.NoError(t, os.WriteFile(target, []byte("original contents"), 0o600))

	_, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"download", "uploads", "missing.txt", target,
	)
	require.Error(t, err)

	preserved, rerr := os.ReadFile(target)
	require.NoError(t, rerr)
	assert.Equal(t, "original contents", string(preserved))

	_, statErr := os.Stat(target + ".part")
	require.True(t, os.IsNotExist(statErr), "expected .part file to be removed after failed download, got err=%v", statErr)
}
