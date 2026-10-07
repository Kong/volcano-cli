package sandboxes

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestCustomDeploymentUploadStatusAndSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM python:3.12-slim\n"), 0o600))
	var uploaded []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "/projects/" + testProject + "/sandboxes/" + testSession + "/deployments"
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == base:
			assert.Equal(t, testRequest, r.Header.Get("Idempotency-Key"))
			if !assert.NoError(t, r.ParseMultipartForm(1<<20)) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer r.MultipartForm.RemoveAll()
			assert.Equal(t, "custom-python", r.FormValue("name"))
			assert.Equal(t, "2048", r.FormValue("memory_mb"))
			assert.Equal(t, "[8080]", r.FormValue("ports"))
			f, _, err := r.FormFile("code")
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer f.Close()
			uploaded, err = io.ReadAll(f)
			assert.NoError(t, err)
			gz, err := gzip.NewReader(bytes.NewReader(uploaded))
			if !assert.NoError(t, err) {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			defer gz.Close()
			header, err := tar.NewReader(gz).Next()
			assert.NoError(t, err)
			assert.Equal(t, "Dockerfile", header.Name)
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"id":"`+testRequest+`","status":"building"}`)
		case r.URL.Path == base+"/"+testRequest+"/source":
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(uploaded)
		case r.URL.Path == base+"/"+testRequest:
			_, _ = io.WriteString(w, `{"id":"`+testRequest+`","status":"active"}`)
		case r.URL.Path == base:
			assert.Equal(t, "next", r.URL.Query().Get("cursor"))
			_, _ = io.WriteString(w, `{"data":[],"pagination":{"limit":10,"has_more":false}}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	run := func(args ...string) []byte {
		t.Helper()
		cmd := New(testDeps(server))
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		require.NoError(t, cmd.Execute())
		return out.Bytes()
	}
	result := run("templates", "deploy", "custom-python", "--template", testSession, "--request-id", testRequest, "--path", dir, "--memory", "2048", "--ports", "8080", "--json")
	var response map[string]any
	require.NoError(t, json.Unmarshal(result, &response))
	require.Equal(t, testSession, response["template_id"])
	require.Contains(t, string(run("deployments", "get", testSession, testRequest)), "active")
	run("deployments", "list", testSession, "--cursor", "next")
	require.Equal(t, uploaded, run("deployments", "source", testSession, testRequest))
}

func TestCustomDeploymentValidationDoesNotSendRequest(t *testing.T) {
	t.Parallel()
	for name, args := range map[string][]string{
		"replay without stable template": {"templates", "deploy", "app", "--request-id", testRequest},
		"invalid port":                   {"templates", "deploy", "app", "--ports", "65536"},
		"invalid memory":                 {"templates", "deploy", "app", "--memory", "512"},
		"invalid name":                   {"templates", "deploy", "../app"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cmd := New(cliruntime.Deps{})
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(args)
			require.Error(t, cmd.Execute())
		})
	}
}
