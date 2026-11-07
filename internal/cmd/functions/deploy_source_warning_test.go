package functions

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// deployWithLavaFlow runs a functions command against an API whose project
// has LavaFlow on for acme/shop's main branch. It returns what the command
// printed on stderr and how many times it read LavaFlow's settings.
func deployWithLavaFlow(
	t *testing.T, newCommand func(cliruntime.Deps) *cobra.Command, deps cliruntime.Deps, args ...string,
) (stderr string, reads int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeFunctionRuntimesCommandResponse(w, r):
		case r.Method == http.MethodGet && r.URL.Path == "/lavaflow/settings":
			reads++
			writeFunctionCommandJSON(t, w, http.StatusOK, map[string]any{
				"project_id": functionProjectID, "enabled": true, "autonomy": "open_pr",
				"repository": map[string]any{"repo_full_name": "acme/shop", "production_branch": "main"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			writeFunctionCommandJSON(t, w, http.StatusCreated, functionCommandPayload(functionID, r.FormValue("name")))
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+functionProjectID+"/functions/batch":
			writeFunctionCommandJSON(t, w, http.StatusAccepted, map[string]any{
				"batch_id": "77777777-7777-4777-8777-000000000003",
				"data": []any{
					functionCommandPayload(fmt.Sprintf("88888888-8888-4888-8888-%012d", 1), "hello"),
					functionCommandPayload(fmt.Sprintf("88888888-8888-4888-8888-%012d", 2), "orders"),
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps.HTTPClient, deps.APIBaseURL = server.Client(), server.URL
	_, stderr, err := executeFunctionsCommandSplit(t, newCommand(deps), args...)
	require.NoError(t, err)
	return stderr, reads
}

// A deploy of a pushed commit gets no warning. Once a function changes,
// LavaFlow can't read that one, and the warning names the change.
func TestFunctionsDeployWarnsWhenLavaFlowCantReadAFunction(t *testing.T) {
	clean := shop(t)
	deploycommittest.Git(t, ".", "update-ref", "refs/remotes/origin/main", clean.SHA)

	stderr, reads := deployWithLavaFlow(t, New, cliruntime.Deps{}, "deploy", "--all")
	assert.Empty(t, stderr)
	assert.Equal(t, 1, reads)

	write(t, "volcano/functions/hello.js", `exports.handler = async () => ({ statusCode: 201 });`)
	stderr, _ = deployWithLavaFlow(t, New, cliruntime.Deps{}, "deploy", "--all")
	assert.Equal(t, "Warning: LavaFlow can't read the code deployed for function hello to fix errors it serves: "+
		"the checkout has uncommitted changes (volcano/functions/hello.js).\n"+
		"Deploy from a clean checkout of a commit pushed to acme/shop's main branch, or deploy with a Git push.\n", stderr)
}

func TestFunctionsDeployWarnsAboutAnUnpushedCommit(t *testing.T) {
	shop(t)

	stderr, _ := deployWithLavaFlow(t, New, cliruntime.Deps{}, "deploy", "-f", "hello")
	assert.Empty(t, stderr, "no origin/main to compare with")

	head := deploycommittest.Git(t, ".", "rev-parse", "HEAD")
	write(t, "volcano/functions/hello.js", `exports.handler = async () => ({ statusCode: 201 });`)
	deploycommittest.Git(t, ".", "commit", "--quiet", "-am", "unpushed")
	deploycommittest.Git(t, ".", "update-ref", "refs/remotes/origin/main", head)
	unpushed := deploycommittest.Git(t, ".", "rev-parse", "--short=7", "HEAD")

	stderr, _ = deployWithLavaFlow(t, New, cliruntime.Deps{}, "deploy", "-f", "hello")
	assert.Equal(t, "Warning: LavaFlow can't read this deployment's code to fix errors it serves: "+
		"commit "+unpushed+" isn't on origin/main.\n"+
		"Deploy from a clean checkout of a commit pushed to acme/shop's main branch, or deploy with a Git push.\n", stderr)
}

func TestFunctionsDeployInLocalModeNeverAsksLavaFlow(t *testing.T) {
	shop(t)
	write(t, "volcano/functions/hello.js", `exports.handler = async () => ({ statusCode: 201 });`)

	stderr, reads := deployWithLavaFlow(t, NewLocal, cliruntime.Deps{LocalMode: true}, "deploy", "--all")
	assert.NotContains(t, stderr, "Warning: LavaFlow")
	assert.Zero(t, reads)
}
