package api

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPIE2ECloudFrontends(t *testing.T) {
	env := setupAPIE2E(t, "cloud-frontends")

	env.loginAndUse(t)
	// The routed function builds while the frontend does.
	writeAPIE2ERoutedFunction(t, env.projectDir)
	env.runCloudCLI(t, "functions", "deploy", "-f", "echo").requireSuccess(t, "1/1 functions deployment started")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "functions", "delete", "echo", "--yes")
	})

	frontend := "cli-e2e-" + apiE2ESuffix(t)
	writeAPIE2EFrontend(t, env.projectDir)
	env.runCloudCLI(t, "frontends", "deploy", "--name", frontend, "--path", filepath.Join(env.projectDir, "web")).requireSuccess(t, "deployment started")
	env.runCloudCLI(t, "frontends", "list").requireSuccess(t, frontend)
	first := env.waitForCloudCLIContains(t, apiE2EFrontendDeploymentTimeout, "Status: active", "frontends", "get", frontend)
	writeAPIE2EFrontendVersion(t, env.projectDir, "v2")
	env.runCloudCLI(t, "frontends", "deploy", "--name", frontend, "--path", filepath.Join(env.projectDir, "web")).requireSuccess(t, "deployment started")
	writeAPIE2EFrontendVersion(t, env.projectDir, "v3")
	env.runCloudCLI(t, "frontends", "deploy", "--name", frontend, "--path", filepath.Join(env.projectDir, "web")).requireSuccess(t, "deployment started")
	active := env.waitForFrontendDeployment(t, frontend, cliOutputField(first.output, "Current Deployment:"))
	siteURL := cliOutputField(active.output, "Site URL:")
	waitForAPIE2EFrontendContent(t, siteURL, "Volcano CLI E2E v3")

	requireAPIE2EFrontendRoutes(t, env, frontend, siteURL)

	env.runCloudCLI(t, "frontends", "delete", frontend, "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No frontends deployed", "frontends", "list")
}

// requireAPIE2EFrontendRoutes walks the routes commands against a deployed
// frontend and checks each change with a request through the frontend itself.
func requireAPIE2EFrontendRoutes(t *testing.T, env *apiE2E, frontend, siteURL string) {
	t.Helper()
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "echo")
	env.runCloudCLI(t, "config", "deploy").requireSuccess(t, "Configuration deployed")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "echo")
	env.runCloudCLI(t, "functions", "get", "echo").requireSuccess(t, "Visibility: private")
	env.runCloudCLI(t, "frontends", "routes", "list", frontend).
		requireSuccess(t, fmt.Sprintf("No function routes on frontend %q", frontend))

	// A route reaches its function with no credential, so the function has to
	// be public first.
	env.runCloudCLI(t, "frontends", "routes", "create", frontend, "--path", "/api/echo", "--function", "echo").
		requireFailure(t, "Frontend Function routes require a public Function",
			"make 'echo' public first: volcano cloud functions update echo --visibility public")
	env.runCloudCLI(t, "frontends", "routes", "create", frontend, "--path", "/api/echo", "--function", "missing").
		requireFailure(t, `function "missing" not found`)

	env.runCloudCLI(t, "functions", "update", "echo", "--visibility", "public").requireSuccess(t, "visibility set to public")
	// The API answers a malformed prefix with a bare "invalid request", so the
	// CLI refuses it first and says what a prefix may be.
	env.runCloudCLI(t, "frontends", "routes", "create", frontend, "--path", "/api/echo/", "--function", "echo").
		requireFailure(t, `invalid --path "/api/echo/"`, "does not end with /")
	env.runCloudCLI(t, "frontends", "routes", "create", frontend,
		"--path", "/api/echo", "--function", "echo", "--strip-prefix").
		requireSuccess(t,
			fmt.Sprintf("Route /api/echo on frontend '%s' now forwards to function 'echo'", frontend),
			"Strip prefix: yes",
			"the function must authenticate its own callers")
	env.runCloudCLI(t, "frontends", "routes", "list", frontend).
		requireSuccess(t, "/api/echo", "echo", "public", "Total: 1 route(s)")
	env.runCloudCLI(t, "frontends", "get", frontend).
		requireSuccess(t, "Function routes:", "/api/echo -> echo (public, strip prefix)")
	env.runCloudCLI(t, "functions", "get", "echo").
		requireSuccess(t, "Routed from: "+frontend+" /api/echo")
	waitForAPIE2ERoutedPath(t, siteURL+"/api/echo/ping?x=1", "/ping")

	// A routed function cannot stop being public while the route is there.
	env.runCloudCLI(t, "functions", "update", "echo", "--visibility", "authenticated").
		requireFailure(t, "remove attached Frontend Function routes",
			"frontend routes forward to it; remove them first:",
			"volcano cloud frontends routes delete "+frontend+" /api/echo")
	env.runCloudCLI(t, "functions", "get", "echo").requireSuccess(t, "Visibility: public")

	// update keeps what it is not told to change: strip prefix stays on here.
	env.runCloudCLI(t, "frontends", "routes", "update", frontend, "/api/echo", "--path", "/api/v2").
		requireSuccess(t, fmt.Sprintf("Route /api/v2 on frontend '%s' updated", frontend), "Strip prefix: yes")
	waitForAPIE2ERoutedPath(t, siteURL+"/api/v2/ping", "/ping")
	env.runCloudCLI(t, "frontends", "routes", "update", frontend, "/api/v2", "--strip-prefix=false").
		requireSuccess(t, "Strip prefix: no")
	waitForAPIE2ERoutedPath(t, siteURL+"/api/v2/ping", "/api/v2/ping")
	env.runCloudCLI(t, "frontends", "routes", "update", frontend, "/api/v2").
		requireFailure(t, "specify at least one of --path, --function, or --strip-prefix")
	env.runCloudCLI(t, "frontends", "routes", "delete", frontend, "/api/echo", "--yes").
		requireFailure(t, fmt.Sprintf("frontend %q has no route %q", frontend, "/api/echo"))

	env.runCloudCLI(t, "frontends", "routes", "delete", frontend, "/api/v2", "--yes").
		requireSuccess(t, fmt.Sprintf("Route /api/v2 deleted from frontend '%s'", frontend))
	env.runCloudCLI(t, "frontends", "routes", "list", frontend).
		requireSuccess(t, fmt.Sprintf("No function routes on frontend %q", frontend))
	waitForAPIE2EPathNotRouted(t, siteURL+"/api/v2/ping")
	env.runCloudCLI(t, "functions", "get", "echo").requireNotContains(t, "Routed from:")
	env.runCloudCLI(t, "functions", "update", "echo", "--visibility", "private").requireSuccess(t, "visibility set to private")
}

// writeAPIE2ERoutedFunction writes an HTTP-mode function that answers with the
// path it was given, which is what tells a stripped prefix from a kept one.
func writeAPIE2ERoutedFunction(t *testing.T, projectDir string) {
	t.Helper()
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano", "functions", "echo.js"), `
exports.handler = async (event) => ({
  statusCode: 200,
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ routed_path: event.path }),
});
`)
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano", "volcano-config.yaml"), `
version: 1
functions:
  - name: echo
    invocation_mode: http
`)
}

// waitForAPIE2ERoutedPath waits for a request through the frontend to reach
// the routed function with the path it should see. A route change takes a
// moment to reach every request, so the previous answer is expected for a
// while.
func waitForAPIE2ERoutedPath(t *testing.T, url, path string) {
	t.Helper()
	want := fmt.Sprintf(`"routed_path":%q`, path)
	deadline := time.Now().Add(apiE2EFunctionConvergenceTimeout)
	client := &http.Client{Timeout: 15 * time.Second}
	var status int
	var body string
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			data, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			status, body = resp.StatusCode, string(data)
			if status == http.StatusOK && strings.Contains(body, want) {
				return
			}
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("GET %s did not reach the function with path %q: last status %d: %.500s", url, path, status, body)
}

// waitForAPIE2EPathNotRouted waits for a deleted route's path to be answered
// by the frontend itself instead of by the function.
func waitForAPIE2EPathNotRouted(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(apiE2EFunctionConvergenceTimeout)
	client := &http.Client{Timeout: 15 * time.Second}
	var status int
	var body string
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			data, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			status, body = resp.StatusCode, string(data)
			if status < http.StatusInternalServerError && !strings.Contains(body, `"routed_path"`) {
				return
			}
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("GET %s still reaches the function after its route was deleted: last status %d: %.500s", url, status, body)
}

func writeAPIE2EFrontendVersion(t *testing.T, projectDir, version string) {
	t.Helper()
	writeAPIE2EFile(t, filepath.Join(projectDir, "web", "pages", "index.js"), `
export default function Home() {
  return <main>Volcano CLI E2E `+version+`</main>;
}
`)
}

func cliOutputField(output, prefix string) string {
	for line := range strings.SplitSeq(output, "\n") {
		if value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// waitForFrontendDeployment waits for the latest deployment started after
// previousDeployment to become active. A failed redeploy puts the frontend
// back to active on previousDeployment, so this fails on that with the build
// log instead of passing.
func (e *apiE2E) waitForFrontendDeployment(t *testing.T, frontend, previousDeployment string) cliResult {
	t.Helper()
	if previousDeployment == "" {
		t.Fatal("frontend output did not include Current Deployment")
	}
	deadline := time.Now().Add(apiE2EFrontendDeploymentTimeout)
	var last cliResult
	started := ""
	for time.Now().Before(deadline) {
		last = e.runCloudCLIWithin(t, attemptTimeout(deadline), "frontends", "get", frontend)
		if last.code == 0 {
			current := cliOutputField(last.output, "Current Deployment:")
			pending := cliOutputField(last.output, "Pending Deployment:")
			for _, id := range []string{current, pending} {
				if id != "" && id != previousDeployment {
					started = id
				}
			}
			status := cliOutputField(last.output, "Status:")
			if status == "failed" || (status == "active" && pending == "" && current != started) {
				logArgs := []string{"frontends", "logs", frontend, "--type", "build"}
				if started != "" {
					logArgs = []string{"frontends", "logs", frontend, started, "--type", "build"}
				}
				logs := e.runCloudCLIWithin(t, attemptTimeout(deadline), logArgs...)
				t.Fatalf("frontend %s deployment %q did not become active:\n%s\nbuild logs:\n%s",
					frontend, started, redactCredentials(last.output), redactCredentials(logs.output))
			}
			if status == "active" && pending == "" {
				return last
			}
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("frontend %s did not finish a deployment after %s before %s:\n%s",
		frontend, previousDeployment, apiE2EFrontendDeploymentTimeout, redactCredentials(last.output))
	return last
}

func waitForAPIE2EFrontendContent(t *testing.T, siteURL, expected string) {
	t.Helper()
	if siteURL == "" {
		t.Fatal("frontend output did not include Site URL")
	}
	deadline := time.Now().Add(apiE2EFrontendDeploymentTimeout)
	client := &http.Client{Timeout: 15 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(siteURL)
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if readErr == nil && resp.StatusCode == http.StatusOK && strings.Contains(string(body), expected) {
				return
			}
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("frontend %s did not serve %q before timeout", siteURL, expected)
}
