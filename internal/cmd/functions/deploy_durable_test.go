package functions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// A durable function's source sits in volcano/functions alongside the standard
// ones, and only volcano-config.yaml says which is which. A function's kind is
// fixed at creation, and the server refuses the cross-kind name with a 409 that
// would fail the whole batch, so deploy-all has to leave durable names out.
func TestFunctionsDeployAllSkipsDurableDeclaredFunctions(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "hello.js"), `exports.handler = async () => ({});`))
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "order-pipeline.js"), `exports.handler = async () => ({});`))
	require.NoError(t, writeProjectFile("volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: hello
  - name: order-pipeline
    kind: durable
`))

	var deployed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			var manifest []struct {
				Name string `json:"name"`
			}
			require.NoError(t, json.Unmarshal([]byte(r.FormValue("functions")), &manifest))
			data := make([]any, 0, len(manifest))
			for _, item := range manifest {
				deployed = append(deployed, item.Name)
				data = append(data, functionCommandPayload(functionID, item.Name))
			}
			writeFunctionCommandJSON(t, w, http.StatusAccepted, map[string]any{
				"batch_id": "77777777-7777-4777-8777-777777777777",
				"data":     data,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t,
		New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--all")
	require.NoError(t, err)
	assert.Equal(t, []string{"hello"}, deployed)
	assert.Contains(t, out, "Skipping 1 durable function(s) declared in volcano-config.yaml: order-pipeline")
	assert.Contains(t, out, `Deploy them with "volcano cloud durable deploy --all"`)
}

// Naming a durable function here is a mistake worth catching locally: sent on,
// it would be refused by the server with a message about kinds rather than one
// about which command to use.
func TestFunctionsDeploySingleRefusesADurableDeclaredFunction(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "order-pipeline.js"), `exports.handler = async () => ({});`))
	require.NoError(t, writeProjectFile("volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: order-pipeline
    kind: durable
`))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if writeFunctionRuntimesCommandResponse(w, r) {
			return
		}
		t.Errorf("unexpected request to %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t,
		New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "-f", "order-pipeline")
	require.ErrorContains(t, err, `"order-pipeline" is declared durable in volcano-config.yaml`)
	require.ErrorContains(t, err, "volcano cloud durable deploy -f order-pipeline")
}

// A standard function deploy in a project that also holds durable ones must not
// mention them: nothing was skipped.
func TestFunctionsDeploySingleSaysNothingAboutDurableSiblings(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "hello.js"), `exports.handler = async () => ({});`))
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "order-pipeline.js"), `exports.handler = async () => ({});`))
	require.NoError(t, writeProjectFile("volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: order-pipeline
    kind: durable
`))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			assert.Equal(t, "hello", r.FormValue("name"))
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, "hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t,
		New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "-f", "hello")
	require.NoError(t, err)
	assert.NotContains(t, out, "Skipping")
	assert.Contains(t, out, "Function 'hello' deployment started")
}

// A manifest that cannot be parsed leaves the CLI unable to tell the kinds
// apart. Deploy says so and carries on, since the server refuses a cross-kind
// name anyway; failing here would break deploys that have nothing to do with
// durable functions.
func TestFunctionsDeployAllWarnsOnAnUnreadableManifest(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "hello.js"), `exports.handler = async () => ({});`))
	require.NoError(t, writeProjectFile("volcano-config.yaml", "version: 1\nproject:\n  name: [unterminated\n"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			writeFunctionCommandJSON(t, w, http.StatusAccepted, map[string]any{
				"batch_id": "77777777-7777-4777-8777-777777777777",
				"data":     []any{functionCommandPayload(functionID, "hello")},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	out, err := executeFunctionsCommand(t,
		New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "deploy", "--all")
	require.NoError(t, err)
	assert.Contains(t, out, "Warning: could not read")
	assert.Contains(t, out, "1/1 functions deployment started")
}
