package api

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAPIE2ECloudConfig covers the declarative config workflow end to end
// (E2E matrix items 18-21): full-manifest deploy with ${ENV} interpolation
// and report rendering, pull round-trip with --force semantics, dry-run plus
// exact variables sync, plan-gate validation failure, skipped/missing
// warnings, function visibility levels checked with real invokes, and frontend
// function routes, including a route to a non-public function that fails the
// dry run and applies nothing.
func TestAPIE2ECloudConfig(t *testing.T) {
	env := setupAPIE2E(t, "cloud-config")
	writeAPIE2EBaseProject(t, env.projectDir)
	writeAPIE2ERoutedFunction(t, env.projectDir)
	writeAPIE2EFrontend(t, env.projectDir)

	env.loginAndUse(t)
	// The frontend and both functions build at the same time.
	frontend := "cli-e2e-" + apiE2ESuffix(t)
	env.runCloudCLI(t, "frontends", "deploy", "--name", frontend, "--path", filepath.Join(env.projectDir, "web")).requireSuccess(t, "deployment started")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "frontends", "delete", frontend, "--yes")
	})
	for _, function := range []string{"hello", "echo"} {
		env.runCloudCLI(t, "functions", "deploy", "-f", function).requireSuccess(t, "1/1 functions deployment started")
		t.Cleanup(func() {
			_ = env.runCloudCLI(t, "functions", "delete", function, "--yes")
		})
	}
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "echo")
	active := env.waitForCloudCLIContains(t, apiE2EFrontendDeploymentTimeout, "Status: active", "frontends", "get", frontend)
	siteURL := cliOutputField(active.output, "Site URL:")
	waitForAPIE2EFrontendContent(t, siteURL, "Volcano CLI E2E")

	configBucket := "cli-e2e-config-" + apiE2ESuffix(t)
	env.runCloudCLI(t, "storage", "bucket", "create", configBucket).requireSuccess(t, configBucket)
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "storage", "bucket", "delete", configBucket, "--yes")
	})

	// SMOKE_MESSAGE exists server-side but is absent from the manifest below:
	// the deploy must delete it (variables are fully synced).
	env.runCloudCLI(t, "variables", "deploy").requireSuccess(t, "SMOKE_MESSAGE", "variable(s) saved")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")

	manifestPath := filepath.Join(env.projectDir, "volcano", "volcano-config.yaml")
	interpolatedValue := "interpolated-" + apiE2ESuffix(t)
	secretEnv := []string{"CLI_E2E_CONFIG_SECRET=" + interpolatedValue}

	// Item 18: full manifest (every HOBBY-plan section) with ${ENV} interpolation.
	writeAPIE2EFile(t, manifestPath, fmt.Sprintf(`
version: 1
variables:
  - name: CONFIG_SECRET
    value: ${CLI_E2E_CONFIG_SECRET}
  - name: CONFIG_PLAIN
    value: plain-value
buckets:
  - name: %s
    file_size_limit: 8192
    allowed_mime_types:
      - text/plain
    policies:
      - name: config-read
        operation: SELECT
        definition: "true"
realtime:
  enabled: true
  broadcast_enabled: true
auth:
  rate_limits:
    signup: 42
  password:
    min_length: 15
  email:
    templates:
      confirmation:
        subject: "CLI E2E confirm subject"
functions:
  - name: hello
    visibility: public
  - name: echo
    invocation_mode: http
`, configBucket))

	deploy := env.runCloudCLIWithEnv(t, secretEnv, "config", "deploy")
	deploy.requireSuccess(t,
		"Configuration deployed from volcano-config.yaml",
		"variables:",
		"buckets:",
		"buckets.policies:",
		"realtime:",
		"auth:",
		"functions:",
		"Summary:",
	)
	deploy.requireNotContains(t, "Warning:", "Error:")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")

	env.runCloudCLI(t, "storage", "bucket", "get", configBucket).requireSuccess(t, configBucket, "8.0 KiB", "text/plain")
	env.runCloudCLI(t, "storage", "policy", "get", configBucket, "config-read").requireSuccess(t, "config-read", "SELECT", "true")
	env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: public")
	variables := env.runCloudCLI(t, "variables", "list")
	variables.requireSuccess(t, "CONFIG_SECRET", "CONFIG_PLAIN")
	variables.requireNotContains(t, "SMOKE_MESSAGE")

	// Item 19: pull refuses to overwrite, --force succeeds, the export carries
	// variable names and shared membership but no values or write-only secrets,
	// and re-deploying the pulled file unchanged is a no-op.
	env.runCloudCLI(t, "config", "pull").requireFailure(t, "refusing to overwrite", "--force")
	env.runCloudCLI(t, "config", "pull", "--force").requireSuccess(t, "Configuration written to", "write-only secrets")

	pulled, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read pulled manifest: %v", err)
	}
	pulledText := string(pulled)
	for _, needle := range []string{"version: 1", "CONFIG_SECRET", "CONFIG_PLAIN", "config-read", "Write-only secrets are omitted", "visibility: public"} {
		if !strings.Contains(pulledText, needle) {
			t.Fatalf("pulled manifest missing %q:\n%s", needle, pulledText)
		}
	}
	for _, value := range []string{interpolatedValue, "plain-value"} {
		if strings.Contains(pulledText, value) {
			t.Fatalf("pulled manifest contains variable value %q:\n%s", value, pulledText)
		}
	}

	redeploy := env.runCloudCLI(t, "config", "deploy")
	redeploy.requireSuccess(t, "Configuration deployed from volcano-config.yaml", "Summary: 0 created, 0 updated, 0 deleted")
	redeploy.requireNotContains(t, "Warning:", "Error:")

	requireAPIE2EConfigFunctionVisibility(t, env, manifestPath)
	requireAPIE2EConfigFunctionRoutes(t, env, manifestPath, frontend, siteURL)

	// Item 20a: dry run projects the variable deletion without applying it.
	writeAPIE2EFile(t, manifestPath, `
version: 1
variables:
  - name: CONFIG_SECRET
    value: ${CLI_E2E_CONFIG_SECRET}
`)
	dryRun := env.runCloudCLIWithEnv(t, secretEnv, "config", "deploy", "--dry-run")
	dryRun.requireSuccess(t, "Dry run", "1 deleted")
	dryRun.requireNotContains(t, "Configuration deployed")
	env.runCloudCLI(t, "variables", "list").requireSuccess(t, "CONFIG_PLAIN")

	// Item 20b: the real deploy syncs variables exactly (deletes the extra).
	env.runCloudCLIWithEnv(t, secretEnv, "config", "deploy").requireSuccess(t, "Configuration deployed", "1 deleted")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	afterSync := env.runCloudCLI(t, "variables", "list")
	afterSync.requireSuccess(t, "CONFIG_SECRET")
	afterSync.requireNotContains(t, "CONFIG_PLAIN")

	// Item 20c: a plan-gated manifest (region subset on HOBBY) exits non-zero
	// with the server's 422 error list rendered, and nothing is applied.
	writeAPIE2EFile(t, manifestPath, `
version: 1
project:
  all_regions: false
  selected_regions:
    - us-east-1
variables:
  - name: CONFIG_GATED
    value: must-not-land
`)
	gated := env.runCloudCLI(t, "config", "deploy")
	gated.requireFailure(t, "validation error", "selected_regions customization is only available on SUPERAGENT plan", "nothing was applied")
	env.runCloudCLI(t, "variables", "list").requireNotContains(t, "CONFIG_GATED")

	// Item 21: skipped/missing warnings render prominently but exit 0.
	writeAPIE2EFile(t, manifestPath, `
version: 1
functions:
  - name: ghost-fn
    public: true
`)
	coverage := env.runCloudCLI(t, "config", "deploy")
	coverage.requireSuccess(t,
		`Warning: function "ghost-fn" is declared in the manifest but not deployed`,
		`Warning: function "hello" exists but is not covered by your manifest`,
	)

	// Missing env var fails locally before any upload.
	writeAPIE2EFile(t, manifestPath, `
version: 1
variables:
  - name: BROKEN
    value: ${CLI_E2E_UNSET_VARIABLE}
`)
	env.runCloudCLI(t, "config", "deploy").requireFailure(t, `environment variable "CLI_E2E_UNSET_VARIABLE" is not set`)

	for _, function := range []string{"hello", "echo"} {
		env.runCloudCLI(t, "functions", "delete", function, "--yes").requireSuccess(t, "deletion started")
	}
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No functions deployed", "functions", "list")
	env.runCloudCLI(t, "frontends", "delete", frontend, "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No frontends deployed", "frontends", "list")
}

// requireAPIE2EConfigFunctionVisibility applies each visibility level from the
// manifest, and the deprecated public flag as the level it used to mean, and
// checks each one with real invokes.
func requireAPIE2EConfigFunctionVisibility(t *testing.T, env *apiE2E, manifestPath string) {
	t.Helper()
	functionID := cliOutputField(env.runCloudCLI(t, "functions", "get", "hello").output, "ID:")
	if functionID == "" {
		t.Fatal("functions get printed no ID")
	}
	invokers := newAPIE2EInvokers(t, env)
	deploy := func(entry, visibility, deprecation string) {
		t.Helper()
		writeAPIE2EFile(t, manifestPath, "version: 1\nfunctions:\n  - name: hello\n"+entry)
		deployed := env.runCloudCLI(t, "config", "deploy")
		deployed.requireSuccess(t, "functions: 1 updated")
		if deprecation == "" {
			deployed.requireNotContains(t, "deprecated")
		} else {
			// On stderr, so output a script parses stays clean.
			deployed.requireSuccess(t, "functions.hello.public is deprecated; "+deprecation)
			if strings.Contains(deployed.stdout, "deprecated") {
				t.Fatalf("the deprecation warning reached stdout:\n%s", deployed.stdout)
			}
		}
		env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: "+visibility)
		env.waitForFunctionVisibility(t, functionID, invokers, visibility)
	}
	deploy("    visibility: authenticated\n", "authenticated", "")
	deploy("    visibility: private\n", "private", "")
	// public: false used to keep anon keys out and let signed-in users in.
	deploy("    public: false\n", "authenticated", "`public: false` sets visibility authenticated")
	deploy("    public: true\n", "public", "`public: true` sets visibility public")
	deploy("    visibility: private\n    public: false\n", "private", "visibility sets the level, so remove public")

	writeAPIE2EFile(t, manifestPath, "version: 1\nfunctions:\n  - name: hello\n    visibility: public\n    public: false\n")
	env.runCloudCLI(t, "config", "deploy").
		requireFailure(t, "nothing was applied", "visibility and the deprecated public flag disagree")
	writeAPIE2EFile(t, manifestPath, "version: 1\nfunctions:\n  - name: hello\n    visibility: everyone\n")
	env.runCloudCLI(t, "config", "deploy").
		requireFailure(t, `function "hello": unsupported visibility "everyone" (expected "private", "authenticated", or "public")`)
	env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: private")
	env.waitForFunctionVisibility(t, functionID, invokers, "private")
}

// requireAPIE2EConfigFunctionRoutes manages a frontend's routes through the
// manifest, where one apply changes a function's visibility and the routes to
// it together, and checks each apply with a request through the frontend.
func requireAPIE2EConfigFunctionRoutes(t *testing.T, env *apiE2E, manifestPath, frontend, siteURL string) {
	t.Helper()
	writeManifest := func(visibility, routes string) {
		t.Helper()
		writeAPIE2EFile(t, manifestPath, fmt.Sprintf(`
version: 1
functions:
  - name: echo
    visibility: %s
    invocation_mode: http
frontends:
  - name: %s
    function_routes:%s
`, visibility, frontend, routes))
	}
	route := `
      - function: echo
        path_prefix: /api/config
        strip_prefix: true`
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "echo").
		requireSuccess(t, "Visibility: private")

	// A route to a function the apply leaves non-public is a validation error:
	// the dry run reports it, and the apply writes nothing, not even the
	// visibility the same manifest declares.
	writeManifest("authenticated", route)
	for _, args := range [][]string{{"config", "deploy", "--dry-run"}, {"config", "deploy"}} {
		refused := env.runCloudCLI(t, args...)
		refused.requireFailure(t,
			"validation error(s); nothing was applied",
			fmt.Sprintf(`frontends.function_routes "%s:/api/config"`, frontend),
			`Frontend Function routes require a public Function; set visibility: public on "echo"`)
		refused.requireNotContains(t, "Dry run: projected actions", "Configuration deployed")
	}
	env.runCloudCLI(t, "functions", "get", "echo").requireSuccess(t, "Visibility: private")
	env.runCloudCLI(t, "frontends", "routes", "list", frontend).
		requireSuccess(t, fmt.Sprintf("No function routes on frontend %q", frontend))
	waitForAPIE2EPathNotRouted(t, siteURL+"/api/config/ping")

	writeManifest("public", route)
	env.runCloudCLI(t, "config", "deploy", "--dry-run").
		requireSuccess(t, "Dry run", "functions: 1 updated", "frontends.function_routes: 1 created")
	env.runCloudCLI(t, "functions", "get", "echo").requireSuccess(t, "Visibility: private")
	env.runCloudCLI(t, "config", "deploy").
		requireSuccess(t, "Configuration deployed", "functions: 1 updated", "frontends.function_routes: 1 created")
	env.runCloudCLI(t, "functions", "get", "echo").
		requireSuccess(t, "Visibility: public", "Routed from: "+frontend+" /api/config")
	waitForAPIE2ERoutedPath(t, siteURL+"/api/config/ping", "/ping")

	// The export writes visibility and the routes back, so re-applying it
	// changes nothing.
	env.runCloudCLI(t, "config", "pull", "--force").requireSuccess(t, "Configuration written to")
	pulled, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read pulled manifest: %v", err)
	}
	for _, needle := range []string{"visibility: public", "function_routes:", "path_prefix: /api/config", "strip_prefix: true"} {
		if !strings.Contains(string(pulled), needle) {
			t.Fatalf("pulled manifest missing %q:\n%s", needle, pulled)
		}
	}
	if strings.Contains(string(pulled), "public: true") {
		t.Fatalf("pulled manifest wrote the deprecated public flag:\n%s", pulled)
	}
	env.runCloudCLI(t, "config", "deploy").
		requireSuccess(t, "Summary: 0 created, 0 updated, 0 deleted")

	// And one apply can drop the route and make the function private again.
	writeManifest("private", " []")
	env.runCloudCLI(t, "config", "deploy").
		requireSuccess(t, "Configuration deployed", "functions: 1 updated", "frontends.function_routes: 1 deleted")
	env.runCloudCLI(t, "frontends", "routes", "list", frontend).
		requireSuccess(t, fmt.Sprintf("No function routes on frontend %q", frontend))
	env.runCloudCLI(t, "functions", "get", "echo").requireSuccess(t, "Visibility: private")
	waitForAPIE2EPathNotRouted(t, siteURL+"/api/config/ping")
}
