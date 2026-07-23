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
// exact variables sync, plan-gate validation failure, and skipped/missing
// warnings.
func TestAPIE2ECloudConfig(t *testing.T) {
	env := setupAPIE2E(t, "cloud-config")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "functions", "deploy", "-f", "hello").requireSuccess(t, "1/1 functions deployment started")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "functions", "delete", "hello", "--yes")
	})

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

	// Item 18: full manifest (every FREE-plan section) with ${ENV} interpolation.
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
    min_length: 12
  email:
    templates:
      confirmation:
        subject: "CLI E2E confirm subject"
functions:
  - name: hello
    public: true
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
	// the interpolated value (variable values are included) but no write-only
	// secrets, and re-deploying the pulled file unchanged is a no-op.
	env.runCloudCLI(t, "config", "pull").requireFailure(t, "refusing to overwrite", "--force")
	env.runCloudCLI(t, "config", "pull", "--force").requireSuccess(t, "Configuration written to", "write-only secrets")

	pulled, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("failed to read pulled manifest: %v", err)
	}
	pulledText := string(pulled)
	for _, needle := range []string{"version: 1", "CONFIG_SECRET", interpolatedValue, "config-read", "Write-only secrets are omitted"} {
		if !strings.Contains(pulledText, needle) {
			t.Fatalf("pulled manifest missing %q:\n%s", needle, pulledText)
		}
	}

	redeploy := env.runCloudCLI(t, "config", "deploy")
	redeploy.requireSuccess(t, "Configuration deployed from volcano-config.yaml", "Summary: 0 created, 0 updated, 0 deleted")
	redeploy.requireNotContains(t, "Warning:", "Error:")

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

	// Item 20c: a plan-gated manifest (region subset on FREE) exits non-zero
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
	gated.requireFailure(t, "validation error", "selected_regions customization is only available on PRO plan", "nothing was applied")
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

	env.runCloudCLI(t, "functions", "delete", "hello", "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No functions deployed", "functions", "list")
}
