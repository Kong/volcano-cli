package durable

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
