package api

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAPIE2ESmokeFunctions(t *testing.T) {
	env := setupAPIE2E(t, "smoke-functions")

	env.loginAndUse(t)
	env.runCloudCLI(t, "functions", "list").requireSuccess(t, "No functions")
}

func TestAPIE2ECloudFunctions(t *testing.T) {
	env := setupAPIE2E(t, "cloud-functions")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "functions", "deploy", "--all").requireSuccess(t, "functions deployment started")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	writeAPIE2EFunctionVersion(t, env.projectDir, "v2")
	env.runCloudCLI(t, "functions", "deploy", "-f", "hello").requireSuccess(t, "1/1 functions deployment started")
	writeAPIE2EFunctionVersion(t, env.projectDir, "v3")
	env.runCloudCLI(t, "functions", "deploy", "-f", filepath.Join("volcano", "functions", "hello.js")).requireSuccess(t, "1/1 functions deployment started")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	env.waitForFunctionVersion(t, "hello", "v3", apiE2EFunctionConvergenceTimeout)
	env.runCloudCLI(t, "functions", "list").requireSuccess(t, "hello")
	env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Name: hello", "Visibility: private")
	env.runCloudCLI(t, "functions", "update", "hello", "--public").requireSuccess(t, "visibility set to public")
	env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: public")
	env.runCloudCLI(t, "functions", "update", "hello", "--private").requireSuccess(t, "visibility set to private")

	env.runCloudCLI(t, "functions", "delete", "hello", "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No functions deployed", "functions", "list")
}

// waitForFunctionVersion waits for the newest deployment to be the one serving.
// A body from an older deployment is expected while the rollout finishes, but an
// invocation that fails is downtime rather than staleness, so the first one is
// reported even when the wanted version turns up afterwards.
func (e *apiE2E) waitForFunctionVersion(t *testing.T, function, version string, timeout time.Duration) {
	t.Helper()
	want := fmt.Sprintf(`"version":%q`, version)
	deadline := time.Now().Add(timeout)

	var last cliResult
	var downtime *cliResult
	converged := false
	for !converged && time.Now().Before(deadline) {
		last = e.runCloudCLIWithin(t, attemptTimeout(deadline), "functions", "invoke", function, "--json")
		switch {
		case last.code != 0:
			if downtime == nil {
				failed := last
				downtime = &failed
			}
		case strings.Contains(last.output, want):
			converged = true
		}
		if !converged {
			time.Sleep(apiE2EPollInterval)
		}
	}

	if !converged {
		t.Fatalf("function %s did not serve %s within %s:\n%s", function, version, timeout, last.output)
	}
	if downtime != nil {
		t.Errorf(
			"function %s failed to invoke with exit code %d while rolling out %s:\n%s",
			function, downtime.code, version, downtime.output,
		)
	}
}

func writeAPIE2EFunctionVersion(t *testing.T, projectDir, version string) {
	t.Helper()
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano", "functions", "hello.js"), `
exports.handler = async () => {
  return { statusCode: 200, body: JSON.stringify({ version: "`+version+`" }) };
};
`)
}
