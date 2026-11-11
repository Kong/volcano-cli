package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// functionPreviewsOffMessage is what the CLI prints for the API's
// feature_unavailable refusal while its function_candidates gate is closed.
const functionPreviewsOffMessage = "function previews aren't available"

// functionPreviewsAvailable reads the server's functions.candidates capability,
// so the test asserts the branch the server is on instead of failing where
// the gate is closed. A server that does not list the capability predates
// function previews, and the test skips.
func functionPreviewsAvailable(t *testing.T, env *apiE2E) bool {
	t.Helper()
	listed := apiE2EJSONRequest(t, http.MethodGet, env.apiURL+"/capabilities", "", nil, http.StatusOK)
	capabilities, ok := listed["data"].([]any)
	if !ok {
		t.Fatalf("capabilities carries no data array: %v", listed)
	}
	for _, entry := range capabilities {
		capability, ok := entry.(map[string]any)
		if ok && capability["id"] == "functions.candidates" {
			return capability["status"] == "available"
		}
	}
	t.Skip("the server does not list functions.candidates; it predates function previews, so they are not checked")
	return false
}

// TestAPIE2ECloudFunctionPreviews walks every preview, serving and rollback
// command against a deployed function: a preview built from the project
// directory serves its own build at its own URL, a promotion moves the
// function's traffic to it, rollbacks move it back, forward and to the
// function's own build, and the refusals a user hits on the way read as the
// API gives them. Where the gate is closed, creating a preview is refused as
// unavailable.
func TestAPIE2ECloudFunctionPreviews(t *testing.T) {
	env := setupAPIE2E(t, "cloud-fn-previews")
	available := functionPreviewsAvailable(t, env)
	writeAPIE2EBaseProject(t, env.projectDir)
	writeAPIE2EFunctionVersion(t, env.projectDir, "own-v1")
	env.loginAndUse(t)

	env.runCloudCLI(t, "functions", "deploy", "-f", "hello").requireSuccess(t, "functions deployment started")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	env.waitForFunctionVersion(t, "hello", "own-v1", apiE2EFunctionConvergenceTimeout)

	if !available {
		env.runCloudCLI(t, "functions", "preview", "create", "hello", "--name", "next").
			requireFailure(t, functionPreviewsOffMessage)
		env.runCloudCLI(t, "functions", "serving", "hello").requireSuccess(t, "Function 'hello' runs its own build")
		return
	}

	serviceKey := createAPIE2EServiceKey(t, env.apiURL, env.token, env.projectID, "cli-e2e-preview-invoker")
	previewURL := createAPIE2EFunctionPreview(t, env, serviceKey)

	revision := env.servingRevision(t, "its own build")
	promoted := env.runCloudCLI(t, "functions", "preview", "promote", "hello", "next")
	promoted.requireSuccess(t, "Function now runs preview 'next'", fmt.Sprintf("Revision: %d", revision+1))
	env.waitForFunctionVersion(t, "hello", "preview-v2", apiE2EFunctionConvergenceTimeout)
	serving := env.runCloudCLI(t, "functions", "serving", "hello")
	serving.requireSuccess(t, fmt.Sprintf("Function 'hello' runs preview 'next' (revision %d)", revision+1),
		fmt.Sprintf("Latest promotion: applied (revision %d)", revision+1), "Image: ")
	env.runCloudCLI(t, "functions", "preview", "list", "hello").requireSuccess(t, "next", "ready", "yes")
	requireAPIE2EServedPreviewRefusals(t, env)

	requireAPIE2EFunctionRollbacks(t, env, revision+1)
	requireAPIE2EPreviewRedeploy(t, env, serviceKey, previewURL)

	env.runCloudCLI(t, "functions", "preview", "delete", "hello", "next", "--yes").
		requireSuccess(t, "Preview 'next' is being deleted")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No previews.", "functions", "preview", "list", "hello")
	env.runCloudCLI(t, "functions", "rollback", "hello").
		requireFailure(t, "there is no earlier runtime to roll back to")
	env.waitForFunctionVersion(t, "hello", "own-v4", apiE2EFunctionConvergenceTimeout)

	env.runCloudCLI(t, "functions", "delete", "hello", "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No functions deployed", "functions", "list")
}

// createAPIE2EFunctionPreview builds the next version of hello as preview
// 'next', waits for it, and checks it answers at its own URL while the
// function keeps its own build. It returns the preview's URL.
func createAPIE2EFunctionPreview(t *testing.T, env *apiE2E, serviceKey string) string {
	t.Helper()
	env.runCloudCLI(t, "functions", "preview", "list", "hello").requireSuccess(t, "No previews.")
	writeAPIE2EFunctionVersion(t, env.projectDir, "preview-v2")
	env.runCloudCLI(t, "functions", "preview", "create", "hello", "--name", "next", "--ttl", "48h").
		requireSuccess(t, "Packaged ", "Preview 'next' deployment started", "functions preview get hello next")
	// The project directory is the function's own source again: a preview is
	// built from whatever is there when it is created or deployed.
	writeAPIE2EFunctionVersion(t, env.projectDir, "own-v1")

	ready := env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status:    ready",
		"functions", "preview", "get", "hello", "next")
	ready.requireSuccess(t, "Name:      next", "Serving:   no", "Variables: current", "(active)", "Image:     ")
	ready.requireNotContains(t, "Expires:   never")
	previewURL := cliOutputField(ready.output, "URL:")
	if !strings.HasPrefix(previewURL, "https://") {
		t.Fatalf("a ready preview shows its URL:\n%s", ready.output)
	}
	env.runCloudCLI(t, "functions", "preview", "list", "hello").
		requireSuccess(t, "NAME", "next", "ready", previewURL)
	waitForAPIE2EPreviewVersion(t, previewURL, serviceKey, "preview-v2")
	env.waitForFunctionVersion(t, "hello", "own-v1", apiE2EFunctionConvergenceTimeout)

	// The API refuses a preview's ID on the function invoke route: a preview is
	// reached only at its own URL.
	previewID := cliOutputField(ready.output, "ID:")
	answer, err := apiE2EInvoke(env.apiURL, serviceKey, previewID)
	if err != nil || answer.status != http.StatusNotFound {
		t.Fatalf("invoking preview %s by ID answered %d %s (err %v), want 404", previewID, answer.status, answer.body, err)
	}
	return previewURL
}

// requireAPIE2EServedPreviewRefusals checks what the CLI says for the
// commands a served preview refuses.
func requireAPIE2EServedPreviewRefusals(t *testing.T, env *apiE2E) {
	t.Helper()
	env.runCloudCLI(t, "functions", "preview", "promote", "hello", "next").
		requireFailure(t, "failed to promote preview \"next\"", "the candidate already serves")
	env.runCloudCLI(t, "functions", "preview", "delete", "hello", "next", "--yes").
		requireFailure(t, "failed to delete preview \"next\"", "the preview is what its frontend or function serves")
	writeAPIE2EFunctionVersion(t, env.projectDir, "preview-v3")
	env.runCloudCLI(t, "functions", "preview", "deploy", "hello", "next").
		requireFailure(t, "failed to deploy preview \"next\"", "the target serves or is kept for a rollback")
	writeAPIE2EFunctionVersion(t, env.projectDir, "own-v1")
	env.runCloudCLI(t, "functions", "preview", "get", "hello", "next").requireSuccess(t, "Serving:   yes")
}

// requireAPIE2EFunctionRollbacks rolls the function back to its own build,
// forward to the preview, and to its own build with --to-production, checking
// what it runs after each. revision is what the promotion made it.
func requireAPIE2EFunctionRollbacks(t *testing.T, env *apiE2E, revision int) {
	t.Helper()
	rolled := env.runCloudCLI(t, "functions", "rollback", "hello")
	rolled.requireSuccess(t, "Function 'hello' rolled back",
		fmt.Sprintf("Function 'hello' runs its own build (revision %d)", revision+1),
		"A rollback returns to preview 'next'", fmt.Sprintf("Latest rollback: applied (revision %d)", revision+1))
	env.waitForFunctionVersion(t, "hello", "own-v1", apiE2EFunctionConvergenceTimeout)
	env.runCloudCLI(t, "functions", "preview", "list", "hello").requireSuccess(t, "rollback")

	forward := env.runCloudCLI(t, "functions", "rollback", "hello")
	forward.requireSuccess(t, fmt.Sprintf("Function 'hello' runs preview 'next' (revision %d)", revision+2))
	env.waitForFunctionVersion(t, "hello", "preview-v2", apiE2EFunctionConvergenceTimeout)

	home := env.runCloudCLI(t, "functions", "rollback", "hello", "--to-production")
	home.requireSuccess(t, "Function 'hello' rolled back",
		fmt.Sprintf("Function 'hello' runs its own build (revision %d)", revision+3),
		"A rollback returns to preview 'next'")
	env.waitForFunctionVersion(t, "hello", "own-v1", apiE2EFunctionConvergenceTimeout)
	env.runCloudCLI(t, "functions", "rollback", "hello", "--to-production").
		requireFailure(t, "failed to roll back function \"hello\"")
}

// requireAPIE2EPreviewRedeploy checks a preview kept for a rollback cannot be
// redeployed, deploys the function, which returns its traffic to its own new
// build and gives the rollback up, then deploys a new build to the preview,
// checks its URL answers with it, promotes it and returns the function to its
// own build.
func requireAPIE2EPreviewRedeploy(t *testing.T, env *apiE2E, serviceKey, previewURL string) {
	t.Helper()
	writeAPIE2EFunctionVersion(t, env.projectDir, "preview-v3")
	env.runCloudCLI(t, "functions", "preview", "deploy", "hello", "next").
		requireFailure(t, "the target serves or is kept for a rollback")

	writeAPIE2EFunctionVersion(t, env.projectDir, "own-v4")
	env.runCloudCLI(t, "functions", "deploy", "-f", "hello").requireSuccess(t, "functions deployment started")
	env.waitForFunctionVersion(t, "hello", "own-v4", apiE2EFunctionDeploymentTimeout)
	// The new Lambda code answers a moment before the deployment is recorded
	// as active, and that record is what releases the preview.
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	env.waitForServingReleased(t, apiE2EFunctionConvergenceTimeout)

	writeAPIE2EFunctionVersion(t, env.projectDir, "preview-v3")
	deployed := env.runCloudCLI(t, "functions", "preview", "deploy", "hello", "next")
	deployed.requireSuccess(t, "Preview 'next' deployment started", "Deployment: ")
	writeAPIE2EFunctionVersion(t, env.projectDir, "own-v4")
	deploymentID := cliOutputField(deployed.output, "Deployment:")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Deployment: "+deploymentID+" (active)",
		"functions", "preview", "get", "hello", "next")
	waitForAPIE2EPreviewVersion(t, previewURL, serviceKey, "preview-v3")
	env.waitForFunctionVersion(t, "hello", "own-v4", apiE2EFunctionConvergenceTimeout)

	env.runCloudCLI(t, "functions", "preview", "promote", "hello", "next").
		requireSuccess(t, "Function now runs preview 'next'")
	env.waitForFunctionVersion(t, "hello", "preview-v3", apiE2EFunctionConvergenceTimeout)
	env.runCloudCLI(t, "functions", "rollback", "hello", "--to-production").
		requireSuccess(t, "Function 'hello' runs its own build")
	env.waitForFunctionVersion(t, "hello", "own-v4", apiE2EFunctionConvergenceTimeout)
}

// waitForServingReleased waits until 'functions serving' shows the function
// on its own build with no preview kept for a rollback, as a deployment of the
// function leaves it.
func (e *apiE2E) waitForServingReleased(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last cliResult
	for time.Now().Before(deadline) {
		last = e.runCloudCLIWithin(t, attemptTimeout(deadline), "functions", "serving", "hello")
		if last.code == 0 && strings.Contains(last.output, "Function 'hello' runs its own build") &&
			!strings.Contains(last.output, "A rollback returns to") {
			return
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("a deployment of hello did not release the preview kept for a rollback within %s:\n%s", timeout, last.output)
}

var servingRevisionPattern = regexp.MustCompile(`\(revision (\d+)\)`)

// servingRevision reads the revision 'functions serving' reports and checks
// the function runs what is expected.
func (e *apiE2E) servingRevision(t *testing.T, runs string) int {
	t.Helper()
	serving := e.runCloudCLI(t, "functions", "serving", "hello")
	serving.requireSuccess(t, "Function 'hello' runs "+runs)
	match := servingRevisionPattern.FindStringSubmatch(serving.output)
	if match == nil {
		t.Fatalf("functions serving shows no revision:\n%s", serving.output)
	}
	revision, err := strconv.Atoi(match[1])
	if err != nil {
		t.Fatalf("parse revision %q: %v", match[1], err)
	}
	return revision
}

// waitForAPIE2EPreviewVersion waits until the preview's own URL answers with
// version. A new host's DNS record takes a moment to resolve.
func waitForAPIE2EPreviewVersion(t *testing.T, previewURL, serviceKey, version string) {
	t.Helper()
	want := fmt.Sprintf(`\"version\":\"%s\"`, version)
	deadline := time.Now().Add(apiE2EFunctionDeploymentTimeout)
	var status int
	var body string
	var err error
	for time.Now().Before(deadline) {
		status, body, err = apiE2EInvokeURL(previewURL, serviceKey)
		if err == nil && status == http.StatusOK && (strings.Contains(body, want) || strings.Contains(body, `"version":"`+version+`"`)) {
			return
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("preview %s never answered %s: last %d %s (err %v)", previewURL, version, status, body, err)
}

// apiE2EInvokeURL invokes a function host, such as a preview's URL, the way a
// caller with its URL does.
func apiE2EInvokeURL(target, token string) (int, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(target, "/")+"/",
		strings.NewReader(`{"payload":{}}`))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(body)), err
}
