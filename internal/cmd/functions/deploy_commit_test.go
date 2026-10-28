package functions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// writeShop writes a project in the working directory and commits it to a
// repository whose origin is acme/shop.
func writeShop(t *testing.T) {
	t.Helper()
	for path, content := range map[string]string{
		"package.json":                          `{"name":"shop","private":true}`,
		"volcano/functions/hello.js":            `exports.handler = async () => ({ statusCode: 200 });`,
		"volcano/functions/orders/index.js":     "exports.handler = async () => ({ statusCode: 200 });\n",
		"volcano/functions/orders/package.json": `{"name":"orders","private":true}`,
	} {
		require.NoError(t, writeProjectFile(path, content))
	}
	deploycommittest.Repository(t, ".", "https://github.com/acme/shop.git")
}

func commitDeps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
}

// A function upload reports no commit, even from a clean GitHub checkout.
func TestFunctionsDeploySingleSendsNoCommit(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	writeShop(t)

	var fields map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			fields = r.MultipartForm.Value
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, "orders"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(commitDeps(server)), "deploy", "-f", "volcano/functions/orders/index.js")
	require.NoError(t, err)
	require.NotNil(t, fields)
	assert.NotContains(t, fields, "commit")
}

func TestFunctionsDeployBatchSendsNoCommit(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	writeShop(t)

	var fields map[string][]string
	var manifest []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			require.NoError(t, r.ParseMultipartForm(32*1024*1024))
			fields = r.MultipartForm.Value
			require.NoError(t, json.Unmarshal([]byte(r.FormValue("functions")), &manifest))
			data := make([]any, 0, len(manifest))
			for i := range manifest {
				var name string
				require.NoError(t, json.Unmarshal(manifest[i]["name"], &name))
				data = append(data, functionCommandPayload(fmt.Sprintf("88888888-8888-4888-8888-%012d", i), name))
			}
			writeFunctionCommandJSON(t, w, http.StatusAccepted, map[string]any{
				"batch_id": "77777777-7777-4777-8777-000000000003",
				"data":     data,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(commitDeps(server)), "deploy", "--all")
	require.NoError(t, err)
	assert.NotContains(t, fields, "commit")
	require.Len(t, manifest, 2)
	for _, entry := range manifest {
		assert.NotContains(t, entry, "commit", string(entry["name"]))
	}
}
