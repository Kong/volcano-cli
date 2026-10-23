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

// writeShop writes a project in the working directory, laid out as a real one
// is, and commits it to a repository whose origin is acme/shop. It then adds
// what a developer's checkout holds that the commit does not: dependencies,
// build output and local env files, all of which git ignores. It returns the
// commit.
func writeShop(t *testing.T) string {
	t.Helper()
	for path, content := range map[string]string{
		".gitignore":                            "node_modules/\ndist/\n.env*\n!.env.example\n",
		".env.example":                          "PAYMENTS_URL=\n",
		"package.json":                          `{"name":"shop","private":true}`,
		"volcano/functions/hello.js":            `exports.handler = async () => ({ statusCode: 200 });`,
		"volcano/functions/orders/index.js":     "const { total } = require(\"../_shared/money\");\nexports.handler = async () => ({ statusCode: 200, body: String(total([])) });\n",
		"volcano/functions/orders/package.json": `{"name":"orders","private":true}`,
		"volcano/functions/_shared/money.js":    "exports.total = (items) => items.length;\n",
	} {
		require.NoError(t, writeProjectFile(path, content))
	}
	commit := deploycommittest.Repository(t, ".", "https://github.com/acme/shop.git")
	for path, content := range map[string]string{
		"node_modules/left-pad/index.js":                   "module.exports = 1;\n",
		"volcano/functions/orders/node_modules/x/index.js": "module.exports = 1;\n",
		"volcano/functions/orders/dist/bundle.js":          "built\n",
		"volcano/functions/orders/.env":                    "PAYMENTS_KEY=secret\n",
		".env.local":                                       "PAYMENTS_KEY=secret\n",
	} {
		require.NoError(t, writeProjectFile(path, content))
	}
	return commit
}

func commitJSON(sha string, dirty bool) string {
	return fmt.Sprintf(`{"repository":"acme/shop","sha":%q,"dirty":%t}`, sha, dirty)
}

func commitDeps(server *httptest.Server) cliruntime.Deps {
	return cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
}

// An honest deploy from a clean checkout reports the commit clean, and an
// edit to any file the upload holds marks it dirty.
func TestFunctionsDeploySingleSendsTheCommit(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	sha := writeShop(t)

	var commit string
	var archive []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			commit = r.FormValue("commit")
			archive = multipartArchiveNames(t, r.MultipartForm.File["code"][0])
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, "orders"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(commitDeps(server)), "deploy", "-f", "volcano/functions/orders/index.js")
	require.NoError(t, err)
	assert.JSONEq(t, commitJSON(sha, false), commit)
	assert.ElementsMatch(t, []string{"index.js", "package.json", "_shared/money.js"}, archive, "ignored files stay out")

	require.NoError(t, writeProjectFile("volcano/functions/_shared/money.js", "exports.total = () => 0;\n"))
	_, err = executeFunctionsCommand(t, New(commitDeps(server)), "deploy", "-f", "volcano/functions/orders/index.js")
	require.NoError(t, err)
	assert.JSONEq(t, commitJSON(sha, true), commit)
}

func TestFunctionsDeployBatchSendsTheCommitPerFunction(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	sha := writeShop(t)

	var manifest []map[string]json.RawMessage
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			require.NoError(t, r.ParseMultipartForm(32*1024*1024))
			require.NoError(t, json.Unmarshal([]byte(r.FormValue("functions")), &manifest))
			_, hasField := r.MultipartForm.Value["commit"]
			assert.False(t, hasField, "the batch carries the commit in its manifest only")
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
	require.Len(t, manifest, 2)
	for _, entry := range manifest {
		assert.JSONEq(t, commitJSON(sha, false), string(entry["commit"]), string(entry["name"]))
	}

	require.NoError(t, writeProjectFile("volcano/functions/hello.js", `exports.handler = async () => ({ statusCode: 204 });`))
	_, err = executeFunctionsCommand(t, New(commitDeps(server)), "deploy", "--all")
	require.NoError(t, err)
	dirty := map[string]string{}
	for _, entry := range manifest {
		dirty[string(entry["name"])] = string(entry["commit"])
	}
	assert.JSONEq(t, commitJSON(sha, true), dirty[`"hello"`])
	assert.JSONEq(t, commitJSON(sha, false), dirty[`"orders"`], "each function is compared on its own files")
}
