package durable

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// A durable function upload reports no commit, even from a clean GitHub
// checkout.
func TestDurableDeploySendsNoCommit(t *testing.T) {
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
	deploycommittest.Repository(t, ".", "git@github.com:acme/shop.git")

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
	assert.NotContains(t, fields, "commit")
}
