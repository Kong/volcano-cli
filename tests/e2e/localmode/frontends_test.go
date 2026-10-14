package localmode

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The same pins hosting's cloud frontend E2E builds with.
const localModeE2EFrontendPackageJSON = `{
  "name": "web",
  "private": true,
  "scripts": {
    "build": "next build"
  },
  "dependencies": {
    "next": "16.2.7",
    "react": "19.2.4",
    "react-dom": "19.2.4"
  }
}
`

// NEXT_PUBLIC_ values are inlined by next build, so the page only shows the
// marker when the deploy's variable selection reached the build.
const localModeE2EFrontendPage = `export default function Home() {
  return <main>{"frontend says " + process.env.NEXT_PUBLIC_SMOKE_FRONTEND}</main>;
}
`

const localModeE2EFrontendMarker = "frontend says hello-from-local-frontend"

// A local build runs npm install and next build inside the server container.
const localModeE2EFrontendBuildTimeout = 10 * time.Minute

func writeLocalModeE2EFrontend(t *testing.T, dir, appDir string) {
	t.Helper()
	writeLocalModeE2EFile(t, dir, filepath.Join(appDir, "package.json"), localModeE2EFrontendPackageJSON)
	writeLocalModeE2EFile(t, dir, filepath.Join(appDir, "pages", "index.js"), localModeE2EFrontendPage)
}

// requireLocalModeServesFrontends deploys a frontend with the local tree,
// fetches it from the local frontend ingress, and runs the rest of the
// lifecycle the local tree offers.
func requireLocalModeServesFrontends(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "--help"), "frontends")

	writeLocalModeE2EFrontend(t, dir, "web")
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "frontend.env"), "NEXT_PUBLIC_SMOKE_FRONTEND=hello-from-local-frontend\n")
	runVolcanoLocalModeE2E(t, binary, env, dir, "variables", "deploy", "-f", filepath.Join("volcano", "frontend.env"))

	deployOutput := runVolcanoLocalModeE2E(t, binary, env, dir,
		"frontends", "deploy", "--name", "web", "--path", "web",
		"--variable-scope", "scoped", "--variable", "NEXT_PUBLIC_SMOKE_FRONTEND")
	requireContains(t, deployOutput, "Frontend 'web' deployment started")
	requireContains(t, deployOutput, "Run 'volcano frontends get web' for the site URL once the build finishes.")
	requireNotContains(t, deployOutput, "deprecated")

	siteURL := awaitLocalModeE2EFrontendActive(t, binary, env, dir, "web")
	awaitLocalModeE2EFrontendServes(t, siteURL, localModeE2EFrontendMarker)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "list"), "web")
	requireLocalModeFrontendRoutes(t, binary, env, dir, siteURL)

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "redeploy", "web"),
		"Frontend 'web' redeploy started")
	awaitLocalModeE2EFrontendServes(t, awaitLocalModeE2EFrontendActive(t, binary, env, dir, "web"), localModeE2EFrontendMarker)

	// npm echoes the script it runs, so this line comes only from the build's
	// own output.
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "logs", "web", "--type", "build"),
		"next build")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "delete", "web", "--yes"),
		"Frontend 'web' deletion started")
	awaitLocalModeE2EFrontendGone(t, binary, env, dir, "web")

	runVolcanoLocalModeE2E(t, binary, env, dir, "variables", "delete", "NEXT_PUBLIC_SMOKE_FRONTEND", "--yes")
	removeLocalModeE2EFile(t, dir, filepath.Join("volcano", "frontend.env"))
}

// requireLocalModeFrontendRoutes forwards a path of the local frontend to a
// function with the routes commands and checks it through the frontend.
func requireLocalModeFrontendRoutes(t *testing.T, binary string, env []string, dir, siteURL string) {
	t.Helper()
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "route-echo.js"), `
exports.handler = async () => ({
  statusCode: 200,
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ routed: true }),
});
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "volcano-config.yaml"), `
version: 1
functions:
  - name: route-echo
    invocation_mode: http
    visibility: public
`)
	runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "deploy", "-f", "route-echo")
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, "Status: active", "functions", "get", "route-echo")
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "config", "deploy"), "Configuration deployed")
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, "Status: active", "functions", "get", "route-echo")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "routes", "list", "web"),
		`No function routes on frontend "web"`)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"frontends", "routes", "create", "web", "--path", "/api/echo", "--function", "route-echo", "--strip-prefix"),
		"Route /api/echo on frontend 'web' now forwards to function 'route-echo'")
	routes := runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "routes", "list", "web")
	requireContains(t, routes, "/api/echo")
	requireContains(t, routes, "route-echo")
	awaitLocalModeE2EFrontendServes(t, strings.TrimSuffix(siteURL, "/")+"/api/echo/ping", `"routed":true`)

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "routes", "delete", "web", "/api/echo", "--yes"),
		"Route /api/echo deleted from frontend 'web'")
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "routes", "list", "web"),
		`No function routes on frontend "web"`)

	runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "delete", "route-echo", "--yes")
	removeLocalModeE2EFile(t, dir, filepath.Join("volcano", "volcano-config.yaml"))
	removeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "route-echo.js"))
}

// requireLocalModeLeavesFrontendDomainsToTheCloud checks that frontend custom
// domains, which local development does not offer, name the cloud command
// instead of running.
func requireLocalModeLeavesFrontendDomainsToTheCloud(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"frontends", "domain", "get", "web"},
		{"frontends", "domain", "create", "web", "--domain", "app.example.com"},
	} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
		if err == nil {
			t.Fatalf("expected volcano %s to fail in local mode\n%s", strings.Join(args, " "), output)
		}
		requireContains(t, output, `"frontends domain" is a cloud command`)
		requireContains(t, output, "volcano cloud frontends domain")
	}
}

// awaitLocalModeE2EFrontendActive waits for the frontend's build to finish and
// returns its site URL.
func awaitLocalModeE2EFrontendActive(t *testing.T, binary string, env []string, dir, name string) string {
	t.Helper()
	deadline := time.Now().Add(localModeE2EFrontendBuildTimeout)
	var last string
	for time.Now().Before(deadline) {
		last = runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "get", name)
		if strings.Contains(last, "Status: failed") {
			logs, _ := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "frontends", "logs", name, "--type", "build")
			t.Fatalf("frontend %s failed to deploy:\n%s\n%s", name, last, logs)
		}
		if strings.Contains(last, "Status: active") && !strings.Contains(last, "Pending Deployment:") {
			return localModeE2EFieldValue(t, last, "Site URL")
		}
		time.Sleep(5 * time.Second)
	}
	t.Fatalf("frontend %s never became active:\n%s", name, last)
	return ""
}

func awaitLocalModeE2EFrontendGone(t *testing.T, binary string, env []string, dir, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		last = runVolcanoLocalModeE2E(t, binary, env, dir, "frontends", "list")
		if !strings.Contains(last, name) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("frontend %s was never deleted:\n%s", name, last)
}

// awaitLocalModeE2EFrontendServes polls the site until it renders want. The
// site is on a *.frontends.localhost name, so requests go to the loopback
// address rather than depend on the host resolving that name.
func awaitLocalModeE2EFrontendServes(t *testing.T, siteURL, want string) {
	t.Helper()
	client := localModeE2EFrontendClient(t, siteURL)

	deadline := time.Now().Add(2 * time.Minute)
	var lastBody string
	var lastStatus int
	for time.Now().Before(deadline) {
		lastBody, lastStatus = fetchLocalModeE2EFrontend(client, siteURL)
		if lastStatus == http.StatusOK && strings.Contains(lastBody, want) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("frontend at %s did not render %q (last status %d):\n%s", siteURL, want, lastStatus, lastBody)
}

func localModeE2EFrontendClient(t *testing.T, siteURL string) *http.Client {
	t.Helper()
	parsed, err := url.Parse(siteURL)
	if err != nil {
		t.Fatalf("frontend site URL %q: %v", siteURL, err)
	}
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, network, net.JoinHostPort("127.0.0.1", parsed.Port()))
			},
		},
	}
}

func fetchLocalModeE2EFrontend(client *http.Client, siteURL string) (string, int) {
	resp, err := client.Get(siteURL)
	if err != nil {
		return err.Error(), 0
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return string(data), resp.StatusCode
}
