package api

import (
	"fmt"
	"maps"
	"net/http"
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
	env.runCloudCLI(t, "functions", "deploy", "--all").requireSuccess(t,
		"functions deployment started",
		"Deployed hello (new, visibility private)",
		"New functions are private",
		"volcano cloud functions update hello --visibility authenticated")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	writeAPIE2EFunctionVersion(t, env.projectDir, "v2")
	redeploy := env.runCloudCLI(t, "functions", "deploy", "-f", "hello")
	redeploy.requireSuccess(t, "1/1 functions deployment started", "Deployed hello")
	redeploy.requireNotContains(t, "(new,", "New functions are private")
	writeAPIE2EFunctionVersion(t, env.projectDir, "v3")
	env.runCloudCLI(t, "functions", "deploy", "-f", filepath.Join("volcano", "functions", "hello.js")).requireSuccess(t, "1/1 functions deployment started")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	env.waitForFunctionVersion(t, "hello", "v3", apiE2EFunctionConvergenceTimeout)
	env.runCloudCLI(t, "functions", "list").requireSuccess(t, "hello")
	get := env.runCloudCLI(t, "functions", "get", "hello")
	get.requireSuccess(t, "Name: hello", "Visibility: private")
	get.requireNotContains(t, "Routed from:")

	requireAPIE2EFunctionVisibility(t, env, cliOutputField(get.output, "ID:"))

	env.runCloudCLI(t, "functions", "delete", "hello", "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No functions deployed", "functions", "list")
}

// apiE2EInvokers are one caller of each kind a visibility level decides on.
type apiE2EInvokers struct {
	serviceKey string
	endUser    string
	anonKey    string
}

// requireAPIE2EFunctionVisibility moves a function through every level with
// `functions update` and checks each one with real invokes, since a level the
// CLI reports but the API does not enforce is the failure that matters.
func requireAPIE2EFunctionVisibility(t *testing.T, env *apiE2E, functionID string) {
	t.Helper()
	if functionID == "" {
		t.Fatal("functions get printed no ID")
	}
	anonKey := createAPIE2EAnonKey(t, env.apiURL, env.token, env.projectID, "cli-e2e-invoker",
		"auth.signup", "auth.signin", "functions.invoke")
	invokers := apiE2EInvokers{
		serviceKey: createAPIE2EServiceKey(t, env.apiURL, env.token, env.projectID, "cli-e2e-invoker"),
		endUser:    signUpAPIE2EEndUser(t, env.apiURL, anonKey),
		anonKey:    anonKey,
	}
	env.waitForFunctionVisibility(t, functionID, invokers, "private")

	for _, level := range []string{"authenticated", "public", "private"} {
		env.runCloudCLI(t, "functions", "update", "hello", "--visibility", level).
			requireSuccess(t, "Function 'hello' visibility set to "+level)
		env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: "+level)
		env.waitForFunctionVisibility(t, functionID, invokers, level)
	}

	// --private used to mean what authenticated means now, so it is refused
	// rather than quietly applying the stricter level.
	env.runCloudCLI(t, "functions", "update", "hello", "--private").
		requireFailure(t, "--private is no longer accepted", "--visibility authenticated")
	env.runCloudCLI(t, "functions", "update", "hello").
		requireFailure(t, "specify --visibility private, authenticated, or public")
	env.runCloudCLI(t, "functions", "update", "hello", "--visibility", "everyone").
		requireFailure(t, `invalid --visibility "everyone"`)
	env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: private")

	env.runCloudCLI(t, "functions", "update", "hello", "--public").
		requireSuccess(t, "Function 'hello' visibility set to public")
	env.waitForFunctionVisibility(t, functionID, invokers, "public")
	env.runCloudCLI(t, "functions", "update", "hello", "--visibility", "private").
		requireSuccess(t, "Function 'hello' visibility set to private")
	env.waitForFunctionVisibility(t, functionID, invokers, "private")
}

// waitForFunctionVisibility waits until every caller gets the answer the level
// gives it. A changed level takes a moment to reach every invoke, so an answer
// from the previous level is expected for a while; a status that neither level
// gives is reported straight away.
func (e *apiE2E) waitForFunctionVisibility(t *testing.T, functionID string, invokers apiE2EInvokers, level string) {
	t.Helper()
	want := map[string]int{
		"service key": http.StatusOK,
		"end user":    http.StatusForbidden,
		"anon key":    http.StatusForbidden,
	}
	if level != "private" {
		want["end user"] = http.StatusOK
	}
	if level == "public" {
		want["anon key"] = http.StatusOK
	}
	tokens := map[string]string{
		"service key": invokers.serviceKey,
		"end user":    invokers.endUser,
		"anon key":    invokers.anonKey,
	}

	deadline := time.Now().Add(apiE2EFunctionConvergenceTimeout)
	var got map[string]int
	for {
		got = make(map[string]int, len(tokens))
		for caller, token := range tokens {
			status, err := apiE2EInvokeStatus(e.apiURL, token, functionID)
			if err != nil {
				t.Fatalf("%s invoking a %s function: %v", caller, level, err)
			}
			if status != http.StatusOK && status != http.StatusForbidden {
				t.Fatalf("%s invoking a %s function got status %d", caller, level, status)
			}
			got[caller] = status
		}
		if maps.Equal(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("invokes of a %s function answered %v, want %v", level, got, want)
		}
		time.Sleep(apiE2EPollInterval)
	}
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
