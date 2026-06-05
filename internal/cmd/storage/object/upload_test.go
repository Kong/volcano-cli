package object

import (
	"errors"
	"io"
	"mime"
	"mime/multipart"
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

func TestUpload(t *testing.T) {
	setObjectCommandTestHome(t)
	saveObjectCommandTestConfig(t)

	dir := t.TempDir()
	localPath := filepath.Join(dir, "hello.txt")
	require.NoError(t, os.WriteFile(localPath, []byte("hi from test"), 0o600))

	var capturedFile []byte
	var capturedContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/storage/uploads/") {
			contentType := r.Header.Get("Content-Type")
			require.True(t, strings.HasPrefix(contentType, "multipart/form-data"), "expected multipart, got %q", contentType)
			_, params, err := mime.ParseMediaType(contentType)
			require.NoError(t, err)
			reader := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := reader.NextPart()
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
				if part.FormName() == "file" {
					capturedContentType = part.Header.Get("Content-Type")
					capturedFile, err = io.ReadAll(part)
					require.NoError(t, err)
				}
			}
			writeObjectJSON(t, w, http.StatusCreated, objectPayload("greetings/hello.txt", 12, false))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeObjectCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"upload", "uploads", localPath, "greetings/hello.txt",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Uploaded")
	assert.Contains(t, out, "greetings/hello.txt")
	assert.Equal(t, "hi from test", string(capturedFile))
	assert.Contains(t, capturedContentType, "text/plain")
}
