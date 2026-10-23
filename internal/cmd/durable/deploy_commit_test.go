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

func TestDurableDeploySendsTheCommit(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	t.Setenv("VOLCANO_DEPLOY_COMMIT", "")
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({});`)
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: order-pipeline
    kind: durable
`)

	var commit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case writeDurableRuntimesResponse(t, w, r):
			return
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+durableProjectID+"/durable-functions":
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			commit = r.FormValue("commit")
			writeDurableCommandJSON(t, w, http.StatusCreated, durableFunctionPayload("order-pipeline", "private"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	deps := cliruntime.Deps{
		CommandPathPrefix: "volcano cloud",
		HTTPClient:        server.Client(),
		APIBaseURL:        server.URL,
		GitCommandRunner:  deploycommittest.Commit(t, "git@github.com:acme/app.git", "."),
	}
	// An edit after the commit makes the upload differ from it.
	writeDurableProjectFile(t, "volcano/functions/order-pipeline.js", `exports.handler = async () => ({ edited: true });`)
	_, err := executeDurableCommand(t, New(deps), "deploy", "--all")
	require.NoError(t, err)
	assert.JSONEq(t, `{"repository":"acme/app","sha":"`+deploycommittest.SHA+`","dirty":true}`, commit)
}
