package functions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const deployCommitJSON = `{"repository":"acme/app","sha":"0123456789abcdef0123456789abcdef01234567","dirty":false}`

// commitDeps answers git as a clean checkout of acme/app would, with the
// machine's own GitHub Actions variables cleared.
func commitDeps(t *testing.T, server *httptest.Server) cliruntime.Deps {
	t.Helper()
	for _, name := range []string{"GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_SERVER_URL", "VOLCANO_DEPLOY_COMMIT"} {
		t.Setenv(name, "")
	}
	return cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		GitCommandRunner: cliruntime.CommandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			switch strings.Join(args[2:], " ") {
			case "remote get-url origin":
				return []byte("https://github.com/acme/app.git\n"), nil
			case "rev-parse HEAD":
				return []byte("0123456789abcdef0123456789abcdef01234567\n"), nil
			case "status --porcelain":
				return nil, nil
			}
			return nil, errors.New("unexpected git call")
		}),
	}
}

func TestFunctionsDeploySingleSendsTheCommit(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "hello.js"), `exports.handler = async () => ({ statusCode: 200 });`))

	var commit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			commit = r.FormValue("commit")
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, "hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := executeFunctionsCommand(t, New(commitDeps(t, server)), "deploy", "-f", "volcano/functions/hello.js")
	require.NoError(t, err)
	assert.JSONEq(t, deployCommitJSON, commit)
}

func TestFunctionsDeployBatchSendsTheCommitPerFunction(t *testing.T) {
	setFunctionCommandTestHome(t)
	saveFunctionCommandTestConfig(t)
	t.Chdir(t.TempDir())
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "one.js"), `exports.handler = async () => ({ statusCode: 200 });`))
	require.NoError(t, writeProjectFile(filepath.Join("volcano", "functions", "two.js"), `exports.handler = async () => ({ statusCode: 200 });`))

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

	_, err := executeFunctionsCommand(t, New(commitDeps(t, server)), "deploy", "--all")
	require.NoError(t, err)
	require.Len(t, manifest, 2)
	for _, entry := range manifest {
		assert.JSONEq(t, deployCommitJSON, string(entry["commit"]))
	}
}
