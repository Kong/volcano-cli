package functions

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// batchManifestItem mirrors the per-function entry the batch endpoint receives.
type batchManifestItem struct {
	Name          string    `json:"name"`
	VariableScope *string   `json:"variable_scope"`
	Variables     *[]string `json:"variables"`
}

func TestFunctionsDeploySingleSendsManifestVariableScope(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "hello.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	require.NoError(t, writeProjectFile("volcano-config.yaml", `version: 1
functions:
  - name: hello
    variable_scope: scoped
    variables:
      - API_KEY
      - DB_URL
`))

	var scope, variables string
	var hasScope, hasVariables bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			_, hasScope = r.MultipartForm.Value["variable_scope"]
			_, hasVariables = r.MultipartForm.Value["variables"]
			scope = r.FormValue("variable_scope")
			variables = r.FormValue("variables")
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, "hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "-f", "volcano/functions/hello.js")
	require.NoError(t, err)
	assert.True(t, hasScope)
	assert.True(t, hasVariables)
	assert.Equal(t, "scoped", scope)
	assert.JSONEq(t, `["API_KEY","DB_URL"]`, variables)
}

// Without a manifest the deploy must be byte-identical to the pre-scoping
// behavior: neither field on the wire, so the server keeps all variables.
func TestFunctionsDeploySingleOmitsScopeWithoutManifest(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "hello.js"), `exports.handler = async () => ({ statusCode: 200 });`))

	var hasScope, hasVariables bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			_, hasScope = r.MultipartForm.Value["variable_scope"]
			_, hasVariables = r.MultipartForm.Value["variables"]
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, "hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "-f", "volcano/functions/hello.js")
	require.NoError(t, err)
	assert.False(t, hasScope)
	assert.False(t, hasVariables)
}

// A function present in volcano/functions/ but absent from the manifest keeps
// its stored scope, so it must send neither field even when a sibling declares one.
func TestFunctionsDeployBatchSendsPerFunctionScope(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "scoped-fn.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "plain-fn.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	require.NoError(t, writeProjectFile("volcano-config.yaml", `version: 1
functions:
  - name: scoped-fn
    variable_scope: scoped
    variables:
      - API_KEY
`))

	var manifest []batchManifestItem
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			require.NoError(t, r.ParseMultipartForm(32*1024*1024))
			require.NoError(t, json.Unmarshal([]byte(r.FormValue("functions")), &manifest))
			data := make([]any, 0, len(manifest))
			for i, item := range manifest {
				data = append(data, functionCommandPayload(fmt.Sprintf("88888888-8888-4888-8888-%012d", i), item.Name))
			}
			writeFunctionCommandJSON(t, w, http.StatusAccepted, map[string]any{
				"batch_id": "77777777-7777-4777-8777-000000000001",
				"data":     data,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--all")
	require.NoError(t, err)
	require.Len(t, manifest, 2)

	byName := make(map[string]batchManifestItem, len(manifest))
	for _, item := range manifest {
		byName[item.Name] = item
	}

	scoped := byName["scoped-fn"]
	require.NotNil(t, scoped.VariableScope)
	assert.Equal(t, "scoped", *scoped.VariableScope)
	require.NotNil(t, scoped.Variables)
	assert.Equal(t, []string{"API_KEY"}, *scoped.Variables)

	plain := byName["plain-fn"]
	assert.Nil(t, plain.VariableScope)
	assert.Nil(t, plain.Variables)
}

// Local mode uploads each function individually rather than as a batch. That
// loop reads the packages built in runDeployAll, so it inherits the same
// declarations; this pins that down.
func TestLocalFunctionsDeployAllSendsPerFunctionScope(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "scoped-fn.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "plain-fn.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	require.NoError(t, writeProjectFile("volcano-config.yaml", `version: 1
functions:
  - name: scoped-fn
    variable_scope: scoped
    variables:
      - API_KEY
`))

	scopes := map[string]string{}
	declared := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			name := r.FormValue("name")
			if values, ok := r.MultipartForm.Value["variable_scope"]; ok {
				scopes[name] = values[0]
			}
			if values, ok := r.MultipartForm.Value["variables"]; ok {
				declared[name] = values[0]
			}
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, name))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, NewLocal(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--all")
	require.NoError(t, err)

	assert.Equal(t, "scoped", scopes["scoped-fn"])
	assert.JSONEq(t, `["API_KEY"]`, declared["scoped-fn"])

	// The undeclared sibling sends neither field.
	assert.NotContains(t, scopes, "plain-fn")
	assert.NotContains(t, declared, "plain-fn")
}
