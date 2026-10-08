package api

import (
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
	waitForAPIE2EFrontendContent(t, cliOutputField(active.output, "Site URL:"), "Volcano CLI E2E v3")
	env.runCloudCLI(t, "frontends", "delete", frontend, "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No frontends deployed", "frontends", "list")
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
