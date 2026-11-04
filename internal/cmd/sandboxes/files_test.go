package sandboxes

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
)

// uploadRequest is one request as the fake server received it.
type uploadRequest struct {
	method, path string
	header       http.Header
	length       int64
	body         []byte
}

// uploadServer records every request and answers the nth, counting from 1,
// with respond.
func uploadServer(t *testing.T, respond func(n int, w http.ResponseWriter)) (*httptest.Server, func() []uploadRequest) {
	t.Helper()
	var mu sync.Mutex
	var requests []uploadRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		requests = append(requests, uploadRequest{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), length: r.ContentLength, body: body})
		n := len(requests)
		mu.Unlock()
		respond(n, w)
	}))
	t.Cleanup(server.Close)
	return server, func() []uploadRequest {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(requests)
	}
}

func respond(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// archived answers an archive with the files and bytes it wrote.
func archived(w http.ResponseWriter, path string, files, size int) {
	respond(w, http.StatusCreated, fmt.Sprintf(`{"project_id":%q,"session_id":%q,"path":%q,"files":%d,"bytes":%d}`,
		testProject, testSession, path, files, size))
}

// archiveForm reads one archive request as the API does: the fields, in
// order, then the archive part. It returns the path field and the archive.
func archiveForm(t *testing.T, r uploadRequest) (path string, archive []byte) {
	t.Helper()
	require.Equal(t, http.MethodPost, r.method)
	require.Equal(t, "/sandbox-file-archives", r.path)
	require.Equal(t, int64(len(r.body)), r.length, "the request declares its length")
	mediaType, params, err := mime.ParseMediaType(r.header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	form := multipart.NewReader(bytes.NewReader(r.body), params["boundary"])
	var names []string
	fields := map[string]string{}
	for {
		part, err := form.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(part)
		require.NoError(t, err)
		names = append(names, part.FormName())
		fields[part.FormName()] = string(data)
	}
	require.Equal(t, []string{"project_id", "session_id", "path", "archive"}, names, "fields go before the archive")
	require.Equal(t, testProject, fields["project_id"])
	require.Equal(t, testSession, fields["session_id"])
	return fields["path"], []byte(fields["archive"])
}

// archiveNames reads one archive request and returns its path field and its
// entry names and file contents.
func archiveNames(t *testing.T, r uploadRequest) (path string, names []string, files map[string]string) {
	t.Helper()
	path, archive := archiveForm(t, r)
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	require.NoError(t, err)
	reader := tar.NewReader(gz)
	files = map[string]string{}
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return path, names, files
		}
		require.NoError(t, err)
		names = append(names, header.Name)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		if header.Typeflag == tar.TypeReg {
			files[header.Name] = string(data)
		}
	}
}

func writeUploadTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "src", "main.py"), []byte("print(42)\n"), 0o644))
	return dir
}

func runUpload(t *testing.T, server *httptest.Server, args ...string) (string, error) {
	t.Helper()
	cmd := New(testDeps(server))
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(append([]string{"files", "upload"}, args...))
	err := cmd.Execute()
	return out.String(), err
}

func TestFilesUploadSendsTheDirectoryAsOneGzipTar(t *testing.T) {
	t.Parallel()
	dir := writeUploadTree(t)
	server, requests := uploadServer(t, func(_ int, w http.ResponseWriter) {
		archived(w, "app/src", 2, 16)
	})

	out, err := runUpload(t, server, testSession, dir, "--path", "app/src")
	require.NoError(t, err)
	assert.Equal(t, "✓ Uploaded 2 files (16B) to /workspace/app/src in 1 call\n", out)

	out, err = runUpload(t, server, testSession, dir, "--path", "app/src", "--json")
	require.NoError(t, err)
	assert.Equal(t, `{"files":2,"bytes":16,"calls":1}`+"\n", out)

	sent := requests()
	require.Len(t, sent, 2)
	for _, request := range sent {
		assert.Equal(t, "Bearer service-key", request.header.Get("Authorization"))
		path, names, files := archiveNames(t, request)
		assert.Equal(t, "app/src", path)
		assert.Equal(t, []string{"README.md", "src/", "src/main.py"}, names)
		assert.Equal(t, map[string]string{"README.md": "hello\n", "src/main.py": "print(42)\n"}, files)
	}
}

func TestFilesUploadSplitsAtTheEntryLimitAndReportsWhatLanded(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// One more file than one call takes.
	for i := range 10_001 {
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%05d", i)), nil, 0o644))
	}
	server, requests := uploadServer(t, func(n int, w http.ResponseWriter) {
		if n == 1 {
			archived(w, "/workspace/data", 10_000, 0)
			return
		}
		respond(w, http.StatusConflict, `{"error":"the destination has a symbolic link or file where the archive needs a directory, or a directory where it has a file","code":"sandbox_path_conflict"}`)
	})

	_, err := runUpload(t, server, testSession, dir, "--path", "/workspace/data")
	require.EqualError(t, err, "upload stopped: the upload conflicts with the session's workspace: --path or a directory "+
		"in the upload is a file or symbolic link there, or a file in the upload is a directory there. "+
		"Earlier calls already wrote 10000 files (0B) to /workspace/data")
	assert.Equal(t, http.StatusConflict, api.Status(err))

	sent := requests()
	require.Len(t, sent, 2)
	_, first, _ := archiveNames(t, sent[0])
	path, second, _ := archiveNames(t, sent[1])
	require.Len(t, first, 10_000)
	assert.Equal(t, "f00000", first[0])
	assert.Equal(t, []string{"f10000"}, second, "archives go in walk order")
	assert.Equal(t, "/workspace/data", path)
}

func TestFilesUploadWordsEachRefusal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status int
		code   string
		want   string
	}{
		{http.StatusBadRequest, "sandbox_archive_invalid", "a path may be too long"},
		{http.StatusRequestEntityTooLarge, "sandbox_archive_too_large", "over its per-call limits; upgrade the CLI"},
		{http.StatusConflict, "sandbox_path_conflict", "conflicts with the session's workspace"},
		{http.StatusRequestEntityTooLarge, "sandbox_workspace_full", "the session's disk has no room for the upload"},
		{http.StatusConflict, "sandbox_guest_outdated", "start a new session from a current preset, or redeploy its template"},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			server, _ := uploadServer(t, func(_ int, w http.ResponseWriter) {
				respond(w, tc.status, fmt.Sprintf(`{"error":"refused","code":%q}`, tc.code))
			})
			_, err := runUpload(t, server, testSession, writeUploadTree(t), "--path", "app")
			require.Error(t, err)
			assert.True(t, strings.HasPrefix(err.Error(), "upload failed: "), err.Error())
			assert.Contains(t, err.Error(), tc.want)
			assert.True(t, strings.HasSuffix(err.Error(), ". Nothing was written"), err.Error())
			assert.Equal(t, tc.status, api.Status(err), "the exit code keeps the API status")
			assert.Equal(t, tc.code, api.Code(err))
		})
	}
	t.Run("server without uploads", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.NotFoundHandler())
		t.Cleanup(server.Close)
		_, err := runUpload(t, server, testSession, writeUploadTree(t), "--path", "app")
		require.EqualError(t, err, "upload failed: this server does not support Sandbox uploads yet. Nothing was written")
	})
	t.Run("a session outside the selected project", func(t *testing.T) {
		t.Parallel()
		server, _ := uploadServer(t, func(_ int, w http.ResponseWriter) {
			respond(w, http.StatusNotFound, `{"error":"sandbox session not found"}`)
		})
		_, err := runUpload(t, server, testSession, writeUploadTree(t), "--path", "app")
		require.EqualError(t, err, "upload failed: no session "+testSession+" in the selected project. Nothing was written")
		assert.Equal(t, http.StatusNotFound, api.Status(err))
	})
	t.Run("other refusals keep the API's words", func(t *testing.T) {
		t.Parallel()
		server, _ := uploadServer(t, func(_ int, w http.ResponseWriter) {
			respond(w, http.StatusForbidden, `{"error":"missing permission sandboxes.files.write"}`)
		})
		_, err := runUpload(t, server, testSession, writeUploadTree(t), "--path", "app")
		require.EqualError(t, err, "HTTP 403: missing permission sandboxes.files.write")
	})
}

func TestFilesUploadRefusesLocallyBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	server, requests := uploadServer(t, func(_ int, w http.ResponseWriter) {
		respond(w, http.StatusInternalServerError, `{"error":"unexpected request"}`)
	})
	t.Cleanup(func() { assert.Empty(t, requests()) })

	t.Run("symbolic link", func(t *testing.T) {
		t.Parallel()
		dir := writeUploadTree(t)
		require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(dir, "src", "passwd")))
		_, err := runUpload(t, server, testSession, dir, "--path", "app")
		require.EqualError(t, err, filepath.Join(dir, "src", "passwd")+" is a symbolic link; uploads copy only regular files and directories")
	})
	t.Run("missing --path", func(t *testing.T) {
		t.Parallel()
		_, err := runUpload(t, server, testSession, writeUploadTree(t))
		require.ErrorContains(t, err, `required flag(s) "path" not set`)
	})
	t.Run("not a directory", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		_, err := runUpload(t, server, testSession, file, "--path", "app")
		require.ErrorContains(t, err, "not a directory")
	})
}
