package durable

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDurableDeploySendsTheCommit(t *testing.T) {
	setDurableCommandTestHome(t)
	t.Chdir(t.TempDir())
	for _, name := range []string{"GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_SERVER_URL", "VOLCANO_DEPLOY_COMMIT"} {
		t.Setenv(name, "")
	}
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
		GitCommandRunner: cliruntime.CommandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			switch strings.Join(args[2:], " ") {
			case "remote get-url origin":
				return []byte("git@github.com:acme/app.git\n"), nil
			case "rev-parse HEAD":
				return []byte("0123456789abcdef0123456789abcdef01234567\n"), nil
			case "status --porcelain":
				return []byte(" M volcano/functions/order-pipeline.js\n"), nil
			}
			return nil, errors.New("unexpected git call")
		}),
	}
	_, err := executeDurableCommand(t, New(deps), "deploy", "--all")
	require.NoError(t, err)
	assert.JSONEq(t, `{"repository":"acme/app","sha":"0123456789abcdef0123456789abcdef01234567","dirty":true}`, commit)
}
