package sandboxes

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/sandbox"
)

type partBody struct {
	ProjectID string `json:"project_id"`
	SessionID string `json:"session_id"`
	Path      string `json:"path"`
	Offset    int64  `json:"offset"`
	Size      int64  `json:"size"`
	Sha256    string `json:"sha256"`
	Data      []byte `json:"data"`
}

// partRequest reads one file part request.
func partRequest(t *testing.T, r uploadRequest) partBody {
	t.Helper()
	require.Equal(t, http.MethodPost, r.method)
	require.Equal(t, "/sandbox-file-parts", r.path)
	var body partBody
	require.NoError(t, json.Unmarshal(r.body, &body))
	require.Equal(t, testProject, body.ProjectID)
	require.Equal(t, testSession, body.SessionID)
	return body
}

// largeUploadTree holds a small file and one incompressible file just over
// what one archive call takes, so it goes in four full parts and one short one.
func largeUploadTree(t *testing.T) (dir string, big []byte) {
	t.Helper()
	dir = t.TempDir()
	big = make([]byte, sandbox.ArchiveLimits.Body+sandbox.PartSize/8)
	_, err := rand.Read(big)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "small"), []byte("small"), 0o644))
	return dir, big
}

func TestFilesUploadSendsAFileTooLargeForAnArchiveInParts(t *testing.T) {
	t.Parallel()
	dir, big := largeUploadTree(t)
	sum := sha256.Sum256(big)
	const parts = 5
	small := func(w http.ResponseWriter) { archived(w, "app", 1, 5) }
	written := func(w http.ResponseWriter) {
		respond(w, http.StatusCreated, `{"project_id":"`+testProject+`","session_id":"`+testSession+`","path":"app/big.bin","offset":0,"size":1,"received":1,"complete":false}`)
	}
	unavailable := func(w http.ResponseWriter) {
		respond(w, http.StatusServiceUnavailable, `{"error":"sandbox operation unavailable"}`)
	}

	t.Run("in order with the whole file's size and SHA-256", func(t *testing.T) {
		t.Parallel()
		server, requests := uploadServer(t, func(n int, w http.ResponseWriter) {
			if n == 1 {
				small(w)
				return
			}
			written(w)
		})
		out, err := runUpload(t, server, testSession, dir, "--path", "app", "--json")
		require.NoError(t, err)
		assert.JSONEq(t, `{"files":2,"bytes":`+strconv.Itoa(5+len(big))+`,"calls":6}`, out)

		sent := requests()
		require.Len(t, sent, 1+parts)
		_, names, _ := archiveNames(t, sent[0])
		assert.Equal(t, []string{"small"}, names, "the archive leaves out the large file")
		var data bytes.Buffer
		for i, request := range sent[1:] {
			body := partRequest(t, request)
			assert.Equal(t, "app/big.bin", body.Path)
			assert.Equal(t, int64(i*sandbox.PartSize), body.Offset)
			assert.Equal(t, int64(len(big)), body.Size)
			assert.Equal(t, hex.EncodeToString(sum[:]), body.Sha256)
			data.Write(body.Data)
		}
		assert.Equal(t, big, data.Bytes())
	})

	t.Run("resends a failed part once", func(t *testing.T) {
		t.Parallel()
		server, requests := uploadServer(t, func(n int, w http.ResponseWriter) {
			switch n {
			case 1:
				small(w)
			case 3:
				unavailable(w)
			default:
				written(w)
			}
		})
		_, err := runUpload(t, server, testSession, dir, "--path", "app")
		require.NoError(t, err)
		sent := requests()
		require.Len(t, sent, 2+parts)
		assert.Equal(t, partRequest(t, sent[2]).Offset, partRequest(t, sent[3]).Offset, "the same part again")
	})

	t.Run("stops when the resent part fails too", func(t *testing.T) {
		t.Parallel()
		server, requests := uploadServer(t, func(n int, w http.ResponseWriter) {
			switch n {
			case 1:
				small(w)
			case 3, 4:
				unavailable(w)
			default:
				written(w)
			}
		})
		_, err := runUpload(t, server, testSession, dir, "--path", "app")
		require.EqualError(t, err, "upload stopped: HTTP 503: sandbox operation unavailable. Earlier calls already wrote 1 file (5B) to /workspace/app")
		assert.Equal(t, http.StatusServiceUnavailable, api.Status(err))
		assert.Len(t, requests(), 4)
	})

	t.Run("a file that changed while uploading", func(t *testing.T) {
		t.Parallel()
		server, requests := uploadServer(t, func(n int, w http.ResponseWriter) {
			switch n {
			case 1:
				small(w)
			case 1 + parts:
				respond(w, http.StatusConflict, `{"error":"the file differs from its size or sha256","code":"sandbox_file_part_mismatch"}`)
			default:
				written(w)
			}
		})
		_, err := runUpload(t, server, testSession, dir, "--path", "app")
		require.EqualError(t, err, "upload stopped: "+filepath.Join(dir, "big.bin")+
			" changed while it was being uploaded; upload again. Earlier calls already wrote 1 file (5B) to /workspace/app")
		assert.Equal(t, "sandbox_file_part_mismatch", api.Code(err))
		assert.Len(t, requests(), 1+parts, "a refusal is not resent")
	})

	t.Run("a refused part is not resent", func(t *testing.T) {
		t.Parallel()
		server, requests := uploadServer(t, func(n int, w http.ResponseWriter) {
			if n == 1 {
				small(w)
				return
			}
			respond(w, http.StatusConflict, `{"error":"conflict","code":"sandbox_path_conflict"}`)
		})
		_, err := runUpload(t, server, testSession, dir, "--path", "app")
		require.ErrorContains(t, err, "upload stopped: the upload conflicts with the session's workspace")
		assert.Len(t, requests(), 2)
	})
}

func TestFilesUploadRefusesAFileOverAGibibyteBeforeAnyRequest(t *testing.T) {
	t.Parallel()
	server, requests := uploadServer(t, func(_ int, w http.ResponseWriter) {
		respond(w, http.StatusInternalServerError, `{"error":"unexpected request"}`)
	})
	dir := writeUploadTree(t)
	file, err := os.Create(filepath.Join(dir, "huge"))
	require.NoError(t, err)
	require.NoError(t, file.Truncate(sandbox.MaxPartedBytes+1))
	require.NoError(t, file.Close())
	_, err = runUpload(t, server, testSession, dir, "--path", "app")
	require.EqualError(t, err, filepath.Join(dir, "huge")+" is too large to upload: files are limited to 1 GiB")
	assert.Empty(t, requests())
}
