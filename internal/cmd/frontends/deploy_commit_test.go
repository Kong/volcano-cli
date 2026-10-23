package frontends

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// The commit is read from the directory being deployed, not the one the CLI
// runs in.
func TestFrontendsDeploySendsTheCommitOfItsPath(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	t.Setenv("VOLCANO_DEPLOY_COMMIT", "")
	projectDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projectDir, "package.json"), []byte(`{"name":"web","dependencies":{"next":"15.5.9"}}`), 0o644))

	var commit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends" {
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			commit = r.FormValue("commit")
			writeFrontendCommandJSON(t, w, http.StatusCreated, frontendCommandPayload(frontendID, "web"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	var dirs []string
	checkout := deploycommittest.Commit(t, "https://github.com/acme/web", projectDir)
	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		GitCommandRunner: cliruntime.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			dirs = append(dirs, args[1])
			return checkout.Run(ctx, name, args...)
		}),
	}
	_, err := executeFrontendsCommand(t, New(deps), "deploy", "--name", "web", "--path", projectDir)
	require.NoError(t, err)
	assert.JSONEq(t, `{"repository":"acme/web","sha":"`+deploycommittest.SHA+`","dirty":false}`, commit)
	require.NotEmpty(t, dirs)
	for _, dir := range dirs {
		assert.Equal(t, filepath.Base(projectDir), filepath.Base(dir))
	}
}
