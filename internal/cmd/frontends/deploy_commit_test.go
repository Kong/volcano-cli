package frontends

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
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
// directory the CLI runs in. The upload is the whole workspace, so its root
// is the repository's.
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
		return fmt.Sprintf(`{"repository":"acme/web","sha":%q,"dirty":%t,"root":""}`, sha, dirty)
	}

	assert.JSONEq(t, want(false), deploy())

	require.NoError(t, os.WriteFile(filepath.Join(root, "packages", "ui", "src", "button.tsx"), []byte("export const Button = () => 1;\n"), 0o644))
	assert.JSONEq(t, want(true), deploy(), "an edit in a workspace package")
	deploycommittest.Git(t, root, "checkout", "--", ".")

	require.NoError(t, os.Remove(filepath.Join(root, "apps", "web", "public", "logo.svg")))
	assert.JSONEq(t, want(true), deploy(), "a deleted file")
}

// A frontend in a subdirectory, deployed from inside it, reports where it sits
// in the repository; a change elsewhere in the repository leaves it clean.
func TestFrontendsDeploySendsTheRootOfASubdirectory(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	root := t.TempDir()
	write := func(path, content string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
	}
	write(".gitignore", "node_modules/\n.next/\n")
	write("api/main.go", "package main\n")
	write("site/package.json", `{"name":"site","dependencies":{"next":"15.5.9"}}`)
	write("site/app/page.tsx", "export default function Page() { return null; }\n")
	sha := deploycommittest.Repository(t, root, "git@github.com:acme/web.git")
	t.Chdir(filepath.Join(root, "site", "app"))

	var commit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends" {
			require.NoError(t, r.ParseMultipartForm(4*1024*1024))
			commit = r.FormValue("commit")
			writeFrontendCommandJSON(t, w, http.StatusCreated, frontendCommandPayload(frontendID, "site"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}
	deploy := func() string {
		t.Helper()
		commit = ""
		_, err := executeFrontendsCommand(t, New(deps), "deploy", "--name", "site", "--path", "..")
		require.NoError(t, err)
		return commit
	}
	want := func(dirty bool) string {
		return fmt.Sprintf(`{"repository":"acme/web","sha":%q,"dirty":%t,"root":"site"}`, sha, dirty)
	}

	assert.JSONEq(t, want(false), deploy())

	write("api/main.go", "package main // edited\n")
	assert.JSONEq(t, want(false), deploy(), "an edit outside the frontend")

	write("site/app/page.tsx", "export default function Page() { return 1; }\n")
	assert.JSONEq(t, want(true), deploy(), "an edit inside it")
}

// deployFrontendWithLavaFlow deploys apps/web of a monorepo against an API
// whose project has LavaFlow on for acme/web's main branch. It returns what
// the command printed on stderr and how many times it read LavaFlow's
// settings.
func deployFrontendWithLavaFlow(
	t *testing.T, newCommand func(cliruntime.Deps) *cobra.Command, deps cliruntime.Deps, root string,
) (stderr string, reads int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/lavaflow/settings":
			reads++
			writeFrontendCommandJSON(t, w, http.StatusOK, map[string]any{
				"project_id": frontendProjectID, "enabled": true, "autonomy": "open_pr",
				"repository": map[string]any{"repo_full_name": "acme/web", "production_branch": "main"},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+frontendProjectID+"/frontends":
			writeFrontendCommandJSON(t, w, http.StatusCreated, frontendCommandPayload(frontendID, "web"))
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
	cmd.SetArgs([]string{"deploy", "--name", "web", "--path", filepath.Join(root, "apps", "web")})
	require.NoError(t, cmd.Execute())
	return errOut.String(), reads
}

// A deploy of a pushed commit gets no warning; an edit in the upload does,
// and names the file.
func TestFrontendsDeployWarnsWhenLavaFlowCantReadTheUpload(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	root, sha := writeMonorepo(t)
	deploycommittest.Git(t, root, "update-ref", "refs/remotes/origin/main", sha)

	stderr, reads := deployFrontendWithLavaFlow(t, New, cliruntime.Deps{}, root)
	assert.Empty(t, stderr)
	assert.Equal(t, 1, reads)

	require.NoError(t, os.WriteFile(filepath.Join(root, "packages", "ui", "src", "button.tsx"), []byte("export const Button = () => 1;\n"), 0o644))
	stderr, _ = deployFrontendWithLavaFlow(t, New, cliruntime.Deps{}, root)
	assert.Equal(t, "Warning: LavaFlow can't read this deployment's code to fix errors it serves: "+
		"the checkout has uncommitted changes (packages/ui/src/button.tsx).\n"+
		"Deploy from a clean checkout of a commit pushed to acme/web's main branch, or deploy with a Git push.\n", stderr)
}

func TestFrontendsDeployInLocalModeNeverAsksLavaFlow(t *testing.T) {
	setFrontendCommandTestHome(t)
	saveFrontendCommandTestConfig(t)
	root, _ := writeMonorepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(root, "packages", "ui", "src", "button.tsx"), []byte("export const Button = () => 1;\n"), 0o644))

	stderr, reads := deployFrontendWithLavaFlow(t, NewLocal, cliruntime.Deps{LocalMode: true}, root)
	assert.NotContains(t, stderr, "Warning: LavaFlow")
	assert.Zero(t, reads)
}
