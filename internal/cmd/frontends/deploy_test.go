package frontends

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestFrontendsDeployUploadsArchive(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "package.json"), []byte(`{"name":"web","dependencies":{"next":"15.5.9"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "page.tsx"), []byte("export default null"), 0o644))

	var fields map[string]string
	var filename string
	var archiveNames []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			fields = map[string]string{
				"name":           r.FormValue("name"),
				"framework":      r.FormValue("framework"),
				"app_root":       r.FormValue("app_root"),
				"variable_scope": r.FormValue("variable_scope"),
			}
			assert.NotContains(t, r.MultipartForm.Value, "variables")
			files := r.MultipartForm.File["archive"]
			require.Len(t, files, 1)
			filename = files[0].Filename
			archiveNames = multipartArchiveNames(t, files[0])
			writeFrontendCommandJSON(t, w, http.StatusCreated, frontendCommandPayload(frontendID, "web"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"deploy", "--name", "web", "--path", projectDir, "--variable-scope", "scoped")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		"name": "web", "framework": "nextjs", "app_root": "", "variable_scope": "scoped",
	}, fields)
	assert.Equal(t, "web.tar.gz", filename)
	assert.Contains(t, archiveNames, "page.tsx")
	assert.Contains(t, archiveNames, "package.json")
	assert.Contains(t, out, "Packaging frontend directory:")
	assert.Contains(t, out, "Archive size:")
	assert.Contains(t, out, "Frontend 'web' deployment started")
}

func TestFrontendsDeployPropagatesAppRoot(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "package.json"), []byte(`{"name":"monorepo","workspaces":["apps/*"]}`), 0o644))
	appDir := filepath.Join(projectDir, "apps", "web")
	require.NoError(t, os.MkdirAll(appDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "package.json"), []byte(`{"name":"web","dependencies":{"next":"15.5.9"}}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(appDir, "page.tsx"), []byte("export default null"), 0o644))

	var sawAppRoot string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends" {
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			sawAppRoot = r.FormValue("app_root")
			writeFrontendCommandJSON(t, w, http.StatusCreated, frontendCommandPayload(frontendID, "web"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := executeFrontendsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"deploy", "--name", "web", "--path", projectDir, "--app-root", "apps/web")
	require.NoError(t, err)
	assert.Equal(t, "apps/web", sawAppRoot)
}

func TestFrontendsDeployRequiresPackageJSON(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "page.tsx"), []byte("export default null"), 0o644))

	_, err := executeFrontendsCommand(t, New(cliruntime.Deps{}),
		"deploy", "--name", "web", "--path", projectDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no package.json")
}

func TestFrontendsDeployRejectsUnsupportedFramework(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "package.json"), []byte(`{"name":"web"}`), 0o644))

	_, err := executeFrontendsCommand(t, New(cliruntime.Deps{}),
		"deploy", "--name", "web", "--path", projectDir, "--framework", "svelte")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported framework")
}

func TestFrontendsDeployValidatesVariableSelection(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "invalid scope", args: []string{"--variable-scope", "shared"}, want: "--variable-scope must be all or scoped"},
		{name: "variable without scope", args: []string{"--variable", "API_KEY"}, want: "--variable requires --variable-scope scoped"},
		{name: "variable with all scope", args: []string{"--variable-scope", "all", "--variable", "API_KEY"}, want: "--variable requires --variable-scope scoped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"deploy", "--name", "web"}, tc.args...)
			_, err := executeFrontendsCommand(t, New(cliruntime.Deps{}), args...)
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
		})
	}
}

func multipartArchiveNames(t *testing.T, fileHeader *multipart.FileHeader) []string {
	t.Helper()
	file, err := fileHeader.Open()
	require.NoError(t, err)
	defer file.Close()
	data, err := io.ReadAll(file)
	require.NoError(t, err)
	gz, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if header.FileInfo().IsDir() {
			continue
		}
		names = append(names, header.Name)
	}
	return names
}
