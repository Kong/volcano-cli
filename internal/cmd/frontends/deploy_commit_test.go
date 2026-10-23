package frontends

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// The commit is read from the directory being deployed, not the one the CLI
// runs in.
func TestFrontendsDeploySendsTheCommitOfItsPath(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	for _, name := range []string{"GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_SERVER_URL", "VOLCANO_DEPLOY_COMMIT"} {
		t.Setenv(name, "")
	}
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
	deps := cliruntime.Deps{
		HTTPClient: server.Client(),
		APIBaseURL: server.URL,
		GitCommandRunner: cliruntime.CommandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
			dirs = append(dirs, args[1])
			switch strings.Join(args[2:], " ") {
			case "remote get-url origin":
				return []byte("https://github.com/acme/web\n"), nil
			case "rev-parse HEAD":
				return []byte("0123456789abcdef0123456789abcdef01234567\n"), nil
			case "status --porcelain":
				return nil, nil
			}
			return nil, errors.New("unexpected git call")
		}),
	}
	_, err := executeFrontendsCommand(t, New(deps), "deploy", "--name", "web", "--path", projectDir)
	require.NoError(t, err)
	assert.JSONEq(t, `{"repository":"acme/web","sha":"0123456789abcdef0123456789abcdef01234567","dirty":false}`, commit)
	require.NotEmpty(t, dirs)
	for _, dir := range dirs {
		assert.Equal(t, filepath.Base(projectDir), filepath.Base(dir))
	}
}
