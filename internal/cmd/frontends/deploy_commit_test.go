package frontends

import (
	"fmt"
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

// writeMonorepo writes a pnpm monorepo whose Next.js app uses a workspace
// package, so the upload is the whole workspace plus the app-path marker the
// CLI writes. It commits it to a repository whose origin is acme/web, adds
// what a developer's checkout holds that git ignores, and returns the root
// and the commit.
func writeMonorepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	write := func(path, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}
	for path, content := range map[string]string{
		".gitignore":                 "node_modules/\n.next/\n.env*\nnext-env.d.ts\n*.tsbuildinfo\n",
		"package.json":               `{"name":"monorepo","private":true,"workspaces":["apps/*","packages/*"]}`,
		"pnpm-workspace.yaml":        "packages: [\"apps/*\", \"packages/*\"]\n",
		"apps/web/package.json":      `{"name":"web","dependencies":{"@acme/ui":"workspace:*","next":"15.5.9"}}`,
		"apps/web/next.config.mjs":   "export default {};\n",
		"apps/web/app/page.tsx":      "export default function Page() { return null; }\n",
		"apps/web/public/logo.svg":   "<svg/>\n",
		"packages/ui/package.json":   `{"name":"@acme/ui"}`,
		"packages/ui/src/button.tsx": "export const Button = () => null;\n",
	} {
		write(path, content)
	}
	sha := deploycommittest.Repository(t, root, "https://github.com/acme/web")
	for path, content := range map[string]string{
		"node_modules/next/package.json":  `{"name":"next"}`,
		"apps/web/.next/cache/build.json": "{}",
		"apps/web/next-env.d.ts":          "/// <reference types=\"next\" />\n",
		"apps/web/.env.local":             "API_KEY=secret\n",
		"apps/web/tsconfig.tsbuildinfo":   "{}",
	} {
		write(path, content)
	}
	return root, sha
}

// An honest deploy from a clean checkout reports the commit clean; an edited
// or deleted file marks it dirty. The commit is read from --path, not the
// directory the CLI runs in.
func TestFrontendsDeploySendsTheCommitOfItsPath(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	root, sha := writeMonorepo(t)
	t.Chdir(t.TempDir())

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
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
	deploy := func() string {
		t.Helper()
		commit = ""
		_, err := executeFrontendsCommand(t, New(deps), "deploy", "--name", "web", "--path", filepath.Join(root, "apps", "web"))
		require.NoError(t, err)
		return commit
	}
	want := func(dirty bool) string {
		return fmt.Sprintf(`{"repository":"acme/web","sha":%q,"dirty":%t}`, sha, dirty)
	}

	assert.JSONEq(t, want(false), deploy())

	require.NoError(t, os.WriteFile(filepath.Join(root, "packages", "ui", "src", "button.tsx"), []byte("export const Button = () => 1;\n"), 0o644))
	assert.JSONEq(t, want(true), deploy(), "an edit in a workspace package")
	deploycommittest.Git(t, root, "checkout", "--", ".")

	require.NoError(t, os.Remove(filepath.Join(root, "apps", "web", "public", "logo.svg")))
	assert.JSONEq(t, want(true), deploy(), "a deleted file")
}
