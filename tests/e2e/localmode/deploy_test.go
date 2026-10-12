package localmode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// A deploy waits for its frontend builds, each of which can take
// localModeE2EFrontendBuildTimeout.
const localModeE2EDeployTimeout = 2 * localModeE2EFrontendBuildTimeout

const localModeE2EDeployManifest = `
version: 1
auth:
  password:
    min_length: 20
functions:
  - name: deploy-greeting
    visibility: public
  - name: deploy-route
    visibility: public
    invocation_mode: http
  - name: deploy-pipeline
    kind: durable
frontends:
  - name: deploy-web
    variable_scope: scoped
    variables:
      - NEXT_PUBLIC_DEPLOY_SMOKE
    function_routes:
      - function: deploy-route
        path_prefix: /api
        strip_prefix: true
`

const localModeE2EDeployFrontendPage = `export default function Home() {
  return <main>{"deploy says " + process.env.NEXT_PUBLIC_DEPLOY_SMOKE}</main>;
}
`

var localModeE2EScriptPattern = regexp.MustCompile(`src="(/_next/static/[^"]+\.js)"`)

// writeLocalModeE2EDeployProject writes a project with something for every
// step. The frontend sits in the layout's default workspace, frontends/<name>.
func writeLocalModeE2EDeployProject(t *testing.T, dir, value string) {
	t.Helper()
	writeLocalModeE2EDeployEnv(t, dir, value)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "migrations", "001_create_deploy_smoke.sql"), `
CREATE TABLE IF NOT EXISTS deploy_smoke (
	id BIGSERIAL PRIMARY KEY
)
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "deploy-greeting.js"), `
exports.handler = async () => ({
	statusCode: 200,
	headers: { "content-type": "application/json" },
	body: JSON.stringify({ greeting: process.env.DEPLOY_SMOKE_GREETING })
});
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "deploy-route.js"), `
exports.handler = async (event) => ({
	statusCode: 200,
	headers: { "content-type": "text/plain" },
	body: "route says " + process.env.DEPLOY_SMOKE_GREETING + " " + event.path
});
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "deploy-pipeline.js"), `
const { withDurableExecution } = require('@aws/durable-execution-sdk-js');

exports.handler = withDurableExecution(async (input, ctx) => {
  return ctx.step('echo', () => input);
});
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "volcano-config.yaml"), localModeE2EDeployManifest)
	writeLocalModeE2EFile(t, dir, filepath.Join(".volcano", "source-layout.json"),
		`{"version":1,"frontends":[{"name":"deploy-web","app_root":""}]}`+"\n")
	writeLocalModeE2EFile(t, dir, filepath.Join("frontends", "deploy-web", "package.json"), localModeE2EFrontendPackageJSON)
	writeLocalModeE2EFile(t, dir, filepath.Join("frontends", "deploy-web", "pages", "index.js"), localModeE2EDeployFrontendPage)
}

func writeLocalModeE2EDeployEnv(t *testing.T, dir, value string) {
	t.Helper()
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "volcano.env"),
		fmt.Sprintf("DEPLOY_SMOKE_GREETING=%s\nNEXT_PUBLIC_DEPLOY_SMOKE=%s\n", value, value))
}

// requireLocalModeDeploysWholeProject deploys a project with one command, then
// deploys it again with a changed variable. The second run starts while the
// variable change is still rolling out to the function and the frontend, so
// it also covers deploys that land on a deployment in progress.
func requireLocalModeDeploysWholeProject(t *testing.T, binary string, env []string) {
	t.Helper()
	dir := localModeE2EProjectDir(t)
	writeLocalModeE2EDeployProject(t, dir, "first-run")

	output := runVolcanoLocalModeE2EDeploy(t, binary, env, dir, "deploy", "--with-migrations")
	requireContains(t, output, "Deploying to local project")
	requireContains(t, output, "1. variables   volcano/volcano.env")
	requireContains(t, output, "2. migrations  volcano/migrations -> database app")
	requireContains(t, output, "3. functions   volcano/functions")
	requireContains(t, output, "4. durable     deploy-pipeline")
	requireContains(t, output, "5. frontends   deploy-web (.volcano/source-layout.json)")
	requireContains(t, output, "6. config      volcano/volcano-config.yaml")
	requireContains(t, output, "Applying 001_create_deploy_smoke.sql... ok")
	requireContains(t, output, "Skipping 1 durable function(s) declared in volcano-config.yaml: deploy-pipeline")
	requireNotContains(t, output, "Deploy them with")
	requireContains(t, output, "✓ deploy-greeting")
	requireContains(t, output, "✓ deploy-route")
	requireContains(t, output, "✓ deploy-pipeline")
	requireContains(t, output, "✓ deploy-web")
	requireContains(t, output, "Deployed in")

	siteURL := deployedLocalModeE2EFrontendURL(t, output, "deploy-web")
	awaitLocalModeE2EFrontendServes(t, siteURL, "deploy says first-run")
	awaitLocalModeE2EFrontendServes(t, siteURL+"/api/hello", "route says first-run /hello")

	info := fetchVolcanoLocalModeE2EInfo(t, env)
	functionID := waitForVolcanoLocalModeE2EFunctionID(t, info, "deploy-greeting")
	waitForVolcanoLocalModeE2EInvokeContains(t, info, functionID, `"greeting":"first-run"`)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "get", "deploy-greeting"), "Visibility: public")
	requireNotContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "list"), "deploy-pipeline")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "get", "deploy-pipeline"), "Status: active")
	started := runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "start", "deploy-pipeline", "--input", `{"order":7}`)
	execution := awaitLocalModeE2EDurableExecution(t, binary, env, dir, "deploy-pipeline", localModeE2EFieldValue(t, started, "ID"))
	requireContains(t, execution, "Status: succeeded")
	requireContains(t, execution, `"order": 7`)

	pulled := filepath.Join(t.TempDir(), "pulled-config.yaml")
	runVolcanoLocalModeE2E(t, binary, env, dir, "config", "pull", "-f", pulled)
	requireContains(t, readLocalModeE2EFile(t, filepath.Dir(pulled), filepath.Base(pulled)), "min_length: 20")

	writeLocalModeE2EDeployEnv(t, dir, "second-run")
	output = runVolcanoLocalModeE2EDeploy(t, binary, env, dir, "deploy")
	requireContains(t, output, "2. migrations  skipped: pass --with-migrations to apply volcano/migrations")
	requireNotContains(t, output, "Applying 001_create_deploy_smoke.sql")
	requireContains(t, output, "Deployed in")
	awaitLocalModeE2EFrontendServes(t, siteURL, "deploy says second-run")
	awaitLocalModeE2EFrontendServes(t, siteURL+"/api/hello", "route says second-run /hello")
	waitForVolcanoLocalModeE2EInvokeContains(t, info, functionID, `"greeting":"second-run"`)
}

// requireLocalModeDeployHonorsItsFlagsAndReportsAFailedBuild covers the runs
// that change what a deploy does: --no-frontends leaves the frontend out,
// --no-wait returns while it still builds, and a build that fails makes the
// run fail and names the command that shows why.
func requireLocalModeDeployHonorsItsFlagsAndReportsAFailedBuild(t *testing.T, binary string, env []string) {
	t.Helper()
	dir := localModeE2EProjectDir(t)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "deploy-flags.js"), `
exports.handler = async () => ({ statusCode: 200, body: "ok" });
`)
	writeLocalModeE2EFile(t, dir, filepath.Join(".volcano", "source-layout.json"),
		`{"version":1,"frontends":[{"name":"deploy-flags-web","app_root":""}]}`+"\n")
	writeLocalModeE2EFile(t, dir, filepath.Join("frontends", "deploy-flags-web", "package.json"), localModeE2EFrontendPackageJSON)
	writeLocalModeE2EFile(t, dir, filepath.Join("frontends", "deploy-flags-web", "pages", "index.js"),
		"export default function Home() {\n  return <main>flags</main>;\n}\n")

	output := runVolcanoLocalModeE2EDeploy(t, binary, env, dir, "deploy", "--no-frontends")
	requireContains(t, output, "5. frontends   skipped: --no-frontends")
	requireContains(t, output, "✓ deploy-flags")
	requireNotContains(t, output, "Deploying frontend")
	requireNotContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "list"), "deploy-flags-web")

	output = runVolcanoLocalModeE2EDeploy(t, binary, env, dir, "deploy", "--no-wait")
	requireContains(t, output, "Deploying frontend deploy-flags-web")
	requireContains(t, output, "Deployments started; not waiting for them to finish")
	requireNotContains(t, output, "Waiting for")
	requireNotContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "get", "deploy-flags-web"), "Status: active")
	awaitLocalModeE2EFrontendActive(t, binary, env, dir, "deploy-flags-web")

	writeLocalModeE2EFile(t, dir, filepath.Join("frontends", "deploy-flags-web", "pages", "index.js"),
		"export default function Home() {\n  return <main>{</main>;\n}\n")
	output, err := runVolcanoLocalModeE2EDeployAllowFailure(binary, env, dir, "deploy")
	if err == nil {
		t.Fatalf("expected volcano deploy to fail on a broken frontend build\n%s", output)
	}
	requireContains(t, output, "✗ deploy-flags-web")
	requireContains(t, output, "volcano frontends logs deploy-flags-web --type build")
	requireContains(t, output, "1 deployment(s) failed: frontend deploy-flags-web")
	requireNotContains(t, output, "Deployed in")
}

// requireLocalModeDeployRefusesTwoVariableSources checks that a refusal
// changes nothing: applying the manifest's variables section would delete the
// variables only the env file sets.
func requireLocalModeDeployRefusesTwoVariableSources(t *testing.T, binary string, env []string) {
	t.Helper()
	dir := localModeE2EProjectDir(t)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "volcano.env"), "DEPLOY_REFUSED=from-env-file\n")
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "volcano-config.yaml"), `
version: 1
variables:
  - name: DEPLOY_REFUSED
    value: from-manifest
`)

	output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "deploy")
	if err == nil {
		t.Fatalf("expected volcano deploy to refuse two variable sources\n%s", output)
	}
	requireContains(t, output, "both volcano/volcano.env and the variables section of volcano/volcano-config.yaml set project variables")
	requireNotContains(t, output, "[1/6]")
	requireNotContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "variables", "list"), "DEPLOY_REFUSED")
}

// requireLocalModeDeploysStarter runs the two commands a new Next.js project's
// next steps print. The starter's app has no package.json of its own: the
// root one builds it with `next build web`.
func requireLocalModeDeploysStarter(t *testing.T, binary string, env []string) {
	t.Helper()
	dir := localModeE2EProjectDir(t)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "init", "nextjs", "--example", "notes"),
		"Run: volcano deploy --with-migrations")

	output := runVolcanoLocalModeE2EDeploy(t, binary, env, dir, "deploy", "--with-migrations")
	requireContains(t, output, "Applying 001_create_notes.sql... ok")
	requireContains(t, output, "Applying 015_notes_updated_at_trigger_create.sql... ok")
	requireContains(t, output, "✓ notes-summary")
	requireContains(t, output, "✓ web")
	requireContains(t, output, "Deployed in")

	siteURL := deployedLocalModeE2EFrontendURL(t, output, "web")
	awaitLocalModeE2EFrontendServes(t, siteURL, "Volcano Notes Demo")
	// The browser client only works if the build saw the variables the
	// frontend selects, which next build inlines into the page's scripts. The
	// env file also holds the service key, which the selection leaves out.
	envFile := readLocalModeE2EFile(t, dir, filepath.Join("volcano", "volcano.env"))
	scripts := localModeE2EFrontendScripts(t, siteURL)
	if !strings.Contains(scripts, localModeE2EEnvValue(t, envFile, "NEXT_PUBLIC_VOLCANO_ANON_KEY")) {
		t.Fatal("the frontend's scripts do not contain NEXT_PUBLIC_VOLCANO_ANON_KEY from volcano/volcano.env")
	}
	if strings.Contains(scripts, localModeE2EEnvValue(t, envFile, "VOLCANO_SERVICE_KEY")) {
		t.Fatal("the frontend's scripts contain VOLCANO_SERVICE_KEY from volcano/volcano.env")
	}
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "get", "notes-summary"), "Visibility: authenticated")
}

func runVolcanoLocalModeE2EDeploy(t *testing.T, binary string, env []string, dir string, args ...string) string {
	t.Helper()
	output, err := runVolcanoLocalModeE2EDeployAllowFailure(binary, env, dir, args...)
	if err != nil {
		t.Fatalf("volcano %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return output
}

func runVolcanoLocalModeE2EDeployAllowFailure(binary string, env []string, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), localModeE2EDeployTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// deployedLocalModeE2EFrontendURL reads a frontend's URL from the list a
// deploy prints once its deployments finish.
func deployedLocalModeE2EFrontendURL(t *testing.T, output, name string) string {
	t.Helper()
	_, list, found := strings.Cut(output, "\nFrontends:\n")
	if found {
		for line := range strings.SplitSeq(list, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == name {
				return fields[1]
			}
		}
	}
	t.Fatalf("deploy printed no URL for frontend %s:\n%s", name, output)
	return ""
}

func localModeE2EEnvValue(t *testing.T, envFile, name string) string {
	t.Helper()
	for line := range strings.SplitSeq(envFile, "\n") {
		if value, found := strings.CutPrefix(line, name+"="); found {
			return value
		}
	}
	t.Fatalf("env file sets no %s:\n%s", name, envFile)
	return ""
}

// localModeE2EFrontendScripts returns the concatenated scripts the frontend's
// home page loads.
func localModeE2EFrontendScripts(t *testing.T, siteURL string) string {
	t.Helper()
	client := localModeE2EFrontendClient(t, siteURL)
	page, status := fetchLocalModeE2EFrontend(client, siteURL)
	if status != http.StatusOK {
		t.Fatalf("frontend at %s answered %d:\n%s", siteURL, status, page)
	}
	base, err := url.Parse(siteURL)
	if err != nil {
		t.Fatalf("frontend site URL %q: %v", siteURL, err)
	}
	scripts := localModeE2EScriptPattern.FindAllStringSubmatch(page, -1)
	if len(scripts) == 0 {
		t.Fatalf("frontend at %s loads no scripts:\n%s", siteURL, page)
	}
	var all strings.Builder
	for _, match := range scripts {
		resp, err := client.Get(base.ResolveReference(&url.URL{Path: match[1]}).String())
		if err != nil {
			t.Fatalf("fetch %s: %v", match[1], err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", match[1], err)
		}
		all.Write(body)
	}
	return all.String()
}
