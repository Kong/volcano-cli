package functions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
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

func TestFunctionsDeploySinglePackagesAndUploads(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	require.NoError(t, writeProjectFile("package.json", `{"dependencies":{"lodash":"^4.17.21"}}`))
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "hello.js"), `exports.handler = async () => ({ statusCode: 200 });`))

	var fields map[string]string
	var filename string
	var archiveNames []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			fields = map[string]string{
				"name":    r.FormValue("name"),
				"runtime": r.FormValue("runtime"),
				"handler": r.FormValue("handler"),
			}
			files := r.MultipartForm.File["code"]
			require.Len(t, files, 1)
			filename = files[0].Filename
			archiveNames = multipartArchiveNames(t, files[0])
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, "hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "-f", "volcano/functions/hello.js")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"name": "hello", "runtime": "nodejs24.x", "handler": "handler"}, fields)
	assert.Equal(t, "hello.tar.gz", filename)
	assert.Contains(t, archiveNames, "index.js")
	assert.Contains(t, archiveNames, "package.json")
	assert.Contains(t, out, "Scanning volcano/functions/")
	assert.Contains(t, out, "Deploying function: hello")
	assert.Contains(t, out, "Function 'hello' deployment started")
	assert.Contains(t, out, "1/1 functions deployment started")
}

func TestFunctionsDeploySingleAcceptsDirectoryEntrypointPath(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "hello", "index.js"), `exports.handler = async () => ({ statusCode: 200 });`))

	var fields map[string]string
	var archiveNames []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			fields = map[string]string{
				"name":    r.FormValue("name"),
				"runtime": r.FormValue("runtime"),
				"handler": r.FormValue("handler"),
			}
			files := r.MultipartForm.File["code"]
			require.Len(t, files, 1)
			archiveNames = multipartArchiveNames(t, files[0])
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, "hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "-f", filepath.Join("volcano", "functions", "hello", "index.js"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"name": "hello", "runtime": "nodejs24.x", "handler": "handler"}, fields)
	assert.Contains(t, archiveNames, "index.js")
	assert.Contains(t, out, "Deploying function: hello")
	assert.Contains(t, out, "Function 'hello' deployment started")
	assert.Contains(t, out, "Runtime: nodejs24.x (detected from index.js)")
}

func TestFunctionsDeployEmptyScanHandlesTargetMode(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		wantErr string
		wantOut string
	}{
		{
			name:    "file",
			args:    []string{"deploy", "--file", "hello"},
			wantErr: `function "hello" not found in volcano/functions/`,
			wantOut: "Scanning volcano/functions/",
		},
		{
			name:    "all",
			args:    []string{"deploy", "--all"},
			wantOut: "No functions found in volcano/functions/",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setFunctionCommandTestHome(t)
			saveFunctionCommandTestConfig(t)
			projectDir := t.TempDir()
			t.Chdir(projectDir)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if writeFunctionRuntimesCommandResponse(w, r) {
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()

			out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), tc.args...)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Contains(t, out, tc.wantOut)
		})
	}
}

func TestFunctionsDeployAllPartialFailureExitsNonZero(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "one.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "two.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	var manifest []struct {
		Name      string `json:"name"`
		FileField string `json:"file_field"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			require.NoError(t, json.Unmarshal([]byte(r.FormValue("functions")), &manifest))
			writeFunctionCommandJSON(t, w, http.StatusMultiStatus, map[string]any{
				"batch_id": "77777777-7777-4777-8777-777777777777",
				"data":     []any{functionCommandPayload(functionID, "one")},
				"failed": []any{
					map[string]any{"name": "two", "error": "failed to start function workflow"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--all")
	require.ErrorContains(t, err, "1/2 functions deployment started")
	assert.Equal(t, []struct {
		Name      string `json:"name"`
		FileField string `json:"file_field"`
	}{{Name: "one", FileField: "code_0"}, {Name: "two", FileField: "code_1"}}, manifest)
	assert.Contains(t, out, "Deployed one")
	assert.Contains(t, out, "Failed two")
	assert.Contains(t, out, "Warning: 1/2 functions deployment started across 1 batch(es); 1 failed before workflow start")
}

func TestFunctionsDeployAllChunksLargeBatches(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	for i := range deployBatchSize + 5 {
		require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", fmt.Sprintf("fn-%03d.js", i)), `exports.handler = async () => ({ statusCode: 200 });`))
	}

	var batchSizes []int
	var batchNames [][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			require.NoError(t, r.ParseMultipartForm(32*1024*1024))
			var manifest []struct {
				Name string `json:"name"`
			}
			require.NoError(t, json.Unmarshal([]byte(r.FormValue("functions")), &manifest))
			batchSizes = append(batchSizes, len(manifest))
			names := make([]string, 0, len(manifest))
			data := make([]any, 0, len(manifest))
			for i, item := range manifest {
				names = append(names, item.Name)
				data = append(data, functionCommandPayload(fmt.Sprintf("88888888-8888-4888-8888-%012d", i), item.Name))
			}
			batchNames = append(batchNames, names)
			writeFunctionCommandJSON(t, w, http.StatusAccepted, map[string]any{
				"batch_id": fmt.Sprintf("77777777-7777-4777-8777-%012d", len(batchSizes)),
				"data":     data,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--all")
	require.NoError(t, err)
	assert.Equal(t, []int{deployBatchSize, 5}, batchSizes)
	require.Len(t, batchNames, 2)
	assert.Equal(t, "fn-000", batchNames[0][0])
	assert.Equal(t, "fn-099", batchNames[0][99])
	assert.Equal(t, "fn-100", batchNames[1][0])
	assert.Equal(t, "fn-104", batchNames[1][4])
	assert.Contains(t, out, "Uploading batch 1-100 of 105")
	assert.Contains(t, out, "Uploading batch 101-105 of 105")
	assert.Contains(t, out, "105/105 functions deployment started across 2 batch(es)")
}

func TestLocalFunctionsDeployAllUsesSingleFunctionUploads(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "one.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "two.js"), `exports.handler = async () => ({ statusCode: 200 });`))

	var uploaded []string
	batchCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			name := r.FormValue("name")
			uploaded = append(uploaded, name)
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, name))
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			batchCalled = true
			http.Error(w, "unexpected batch deploy", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t, NewLocal(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--all")

	require.NoError(t, err)
	assert.False(t, batchCalled)
	assert.Equal(t, []string{"one", "two"}, uploaded)
	assert.Contains(t, out, "Uploading one (1/2)")
	assert.Contains(t, out, "Uploading two (2/2)")
	assert.Contains(t, out, "2/2 functions deployment started")
}

func TestFunctionsDeployValidatesTargetFlags(t *testing.T) {
	_, err := executeFunctionsCommand(t, New(cliruntime.Deps{}), "deploy")
	require.ErrorContains(t, err, "specify either --all")

	_, err = executeFunctionsCommand(t, New(cliruntime.Deps{}), "deploy", "--all", "--file", "hello")
	require.ErrorContains(t, err, "cannot use --all and --file together")
}

func writeFunctionRuntimesCommandResponse(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet || r.URL.Path != "/functions/runtimes" {
		return false
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"runtimes": []any{
			functionRuntimeCommandPayload("nodejs24.x", "nodejs", true, []string{".js", ".mjs"}, "index.js", "handler", []string{"package.json", "package-lock.json", "npm-shrinkwrap.json", "pnpm-lock.yaml", "yarn.lock", ".yarnrc.yml"}),
			functionRuntimeCommandPayload("python3.12", "python", true, []string{".py"}, "main.py", "handler", []string{"requirements.txt"}),
			functionRuntimeCommandPayload("ruby3.4", "ruby", true, []string{".rb"}, "main.rb", "handler", []string{"Gemfile", "Gemfile.lock"}),
		},
	})
	return true
}

func writeProjectFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
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
