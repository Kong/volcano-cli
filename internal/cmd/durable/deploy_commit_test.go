package durable

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// deployDurableCommit deploys every durable function in a project committed
// to acme/shop, after change, and returns the commit the upload reported:
// nil for none.
func deployDurableCommit(t *testing.T, change func(t *testing.T)) (commit, clean *api.FunctionCommit) {
	t.Helper()
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline/index.js", "exports.handler = async () => ({});\n")
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: order-pipeline
    kind: durable
`)
	head := deploycommittest.Repository(t, ".", "git@github.com:acme/shop.git")
	change(t)

	var fields map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			fields = r.MultipartForm.Value
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{CommandPathPrefix: "volcano cloud", HTTPClient: server.Client(), APIBaseURL: server.URL}

	_, err := executeDurableCommand(t, New(deps), "deploy", "--all")
	require.NoError(t, err)
	require.NotNil(t, fields)
	if values, ok := fields["commit"]; ok {
		require.Len(t, values, 1)
		require.NoError(t, json.Unmarshal([]byte(values[0]), &commit))
	}
	return commit, &api.FunctionCommit{Repository: "acme/shop", SHA: head}
}

// A durable function upload from a clean checkout reports HEAD.
func TestDurableDeploySendsTheCommit(t *testing.T) {
	commit, clean := deployDurableCommit(t, func(*testing.T) {})
	assert.Equal(t, clean, commit)
}

func TestDurableDeploySendsNoCommitForADirtyFunction(t *testing.T) {
	commit, _ := deployDurableCommit(t, func(t *testing.T) {
		t.Helper()
		writeDurableProjectFile(t, "volcano/functions/order-pipeline/index.js", "exports.handler = async () => ({ changed: true });\n")
	})
	assert.Nil(t, commit)
}

// deployDurableWithLavaFlow deploys every durable function in a project
// committed to acme/shop, after change, against an API whose project has
// LavaFlow on for acme/shop's main branch. It returns what the command
// printed on stderr and how many times it read LavaFlow's settings.
func deployDurableWithLavaFlow(
	t *testing.T, newCommand func(cliruntime.Deps) *cobra.Command, deps cliruntime.Deps, change func(t *testing.T),
) (stderr string, reads int) {
	t.Helper()
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, "volcano/functions/order-pipeline/index.js", "exports.handler = async () => ({});\n")
	writeDurableProjectFile(t, "volcano-config.yaml", "version: 1\nfunctions:\n  - name: order-pipeline\n    kind: durable\n")
	head := deploycommittest.Repository(t, ".", "git@github.com:acme/shop.git")
	deploycommittest.Git(t, ".", "update-ref", "refs/remotes/origin/main", head)
	change(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
		case r.Method == http.MethodGet && r.URL.Path == "/lavaflow/settings":
			reads++
			writeDurableCommandJSON(t, w, http.StatusOK, map[string]any{
				"project_id": durableProjectID, "enabled": true, "autonomy": "open_pr",
				"repository": map[string]any{"repo_full_name": "acme/shop", "production_branch": "main"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps.HTTPClient, deps.APIBaseURL = server.Client(), server.URL

	cmd := newCommand(deps)
	var stdout, errOut bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{"deploy", "--all"})
	require.NoError(t, cmd.Execute())
	return errOut.String(), reads
}

func TestDurableDeployWarnsWhenLavaFlowCantReadAFunction(t *testing.T) {
	stderr, reads := deployDurableWithLavaFlow(t, New, cliruntime.Deps{}, func(*testing.T) {})
	assert.Empty(t, stderr)
	assert.Equal(t, 1, reads)

	stderr, _ = deployDurableWithLavaFlow(t, New, cliruntime.Deps{}, func(t *testing.T) {
		t.Helper()
		writeDurableProjectFile(t, "volcano/functions/order-pipeline/index.js", "exports.handler = async () => ({ changed: true });\n")
	})
	assert.Equal(t, "Warning: LavaFlow can't read this deployment's code to fix errors it serves: "+
		"the checkout has uncommitted changes (volcano/functions/order-pipeline/index.js).\n"+
		"Deploy from a clean checkout of a commit pushed to acme/shop's main branch, or deploy with a Git push.\n", stderr)
}

func TestDurableDeployInLocalModeNeverAsksLavaFlow(t *testing.T) {
	stderr, reads := deployDurableWithLavaFlow(t, NewLocal, cliruntime.Deps{LocalMode: true}, func(t *testing.T) {
		t.Helper()
		writeDurableProjectFile(t, "volcano/functions/order-pipeline/index.js", "exports.handler = async () => ({ changed: true });\n")
	})
	assert.NotContains(t, stderr, "Warning: LavaFlow")
	assert.Zero(t, reads)
}
