package durable

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// An honest deploy from a clean checkout reports the commit clean, files git
// ignores stay out of the upload, and an edit marks it dirty.
func TestDurableDeploySendsTheCommit(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	writeDurableProjectFile(t, ".gitignore", "node_modules/\ndist/\n.env*\n")
	writeDurableProjectFile(t, "package.json", `{"name":"shop","private":true}`)
	writeDurableProjectFile(t, "volcano/functions/order-pipeline/index.js", "const { charge } = require(\"../_shared/payments\");\nexports.handler = async () => charge();\n")
	writeDurableProjectFile(t, "volcano/functions/order-pipeline/package.json", `{"name":"order-pipeline","private":true}`)
	writeDurableProjectFile(t, "volcano/functions/_shared/payments.js", "exports.charge = async () => ({});\n")
	writeDurableProjectFile(t, "volcano-config.yaml", `version: 1
project:
  name: beta
functions:
  - name: order-pipeline
    kind: durable
`)
	sha := deploycommittest.Repository(t, ".", "git@github.com:acme/shop.git")
	writeDurableProjectFile(t, "volcano/functions/order-pipeline/node_modules/x/index.js", "module.exports = 1;\n")
	writeDurableProjectFile(t, "volcano/functions/order-pipeline/.env", "PAYMENTS_KEY=secret\n")

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
	deps := cliruntime.Deps{CommandPathPrefix: "volcano cloud", HTTPClient: server.Client(), APIBaseURL: server.URL}
	want := func(dirty bool) string {
		return fmt.Sprintf(`{"repository":"acme/shop","sha":%q,"dirty":%t}`, sha, dirty)
	}

	_, err := executeDurableCommand(t, New(deps), "deploy", "--all")
	require.NoError(t, err)
	assert.JSONEq(t, want(false), commit)

	writeDurableProjectFile(t, "volcano/functions/_shared/payments.js", "exports.charge = async () => ({ retried: true });\n")
	_, err = executeDurableCommand(t, New(deps), "deploy", "--all")
	require.NoError(t, err)
	assert.JSONEq(t, want(true), commit)
}
