package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIE2ESmokeVariables(t *testing.T) {
	env := setupAPIE2E(t, "smoke-variables")
	// A second project is past the HOBBY cap this test creates one under.
	env.setUserPlan(t, "SUPERAGENT")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "variables", "deploy").requireSuccess(t, "SMOKE_MESSAGE", "variable(s) saved")
	env.runCloudCLI(t, "variables", "list").requireSuccess(t, "SMOKE_MESSAGE")
	env.runCloudCLI(t, "variables", "get", "SMOKE_MESSAGE").requireSuccess(t, "SMOKE_MESSAGE")

	secondaryProject := "cli-e2e-smoke-secondary-" + apiE2ESuffix(t)
	secondaryProjectID := createAPIE2EProject(t, env.apiURL, env.token, secondaryProject)
	t.Cleanup(func() {
		if err := deleteAPIE2EProject(env.apiURL, env.token, secondaryProjectID); err != nil {
			t.Errorf("delete secondary API E2E project: %v", err)
		}
	})
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "secondary.env"), "SECOND_PROJECT_ONLY=1\n")
	env.runCLI(t, "use", secondaryProject).requireSuccess(t, "Now using project")
	env.runCloudCLI(t, "variables", "deploy", "--file", "secondary.env").requireSuccess(t, "SECOND_PROJECT_ONLY", "variable(s) saved")
	env.runCloudCLI(t, "variables", "list").requireSuccess(t, "SECOND_PROJECT_ONLY")
	env.runCLI(t, "use", env.projectID).requireSuccess(t, "Now using project")
	baseVariables := env.runCloudCLI(t, "variables", "list")
	baseVariables.requireSuccess(t, "SMOKE_MESSAGE")
	baseVariables.requireNotContains(t, "SECOND_PROJECT_ONLY")

	env.runCloudCLI(t, "variables", "delete", "SMOKE_MESSAGE", "--yes").requireSuccess(t, "deleted")
}

// TestAPIE2ESmokeVariablesShared saves variables as not shared, from an env
// file and from the manifest, against a deployed server.
func TestAPIE2ESmokeVariablesShared(t *testing.T) {
	env := setupAPIE2E(t, "smoke-variables-shared")
	env.loginAndUse(t)
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "not-shared.env"), "NOT_SHARED_SMOKE=fake-value\n")
	deploy := func(flags ...string) cliResult {
		t.Helper()
		return env.runCloudCLI(t, append([]string{"variables", "deploy", "--file", "not-shared.env"}, flags...)...)
	}
	shared := func() cliResult {
		t.Helper()
		return env.runCloudCLI(t, "variables", "get", "NOT_SHARED_SMOKE")
	}

	// Saved as not shared, a variable stays that way when the file is deployed
	// again without the flag, until --shared shares it.
	deploy("--shared=false").requireSuccess(t, "NOT_SHARED_SMOKE (saved, not shared)")
	shared().requireSuccess(t, "Shared: no")
	deploy().requireSuccess(t, "NOT_SHARED_SMOKE (saved)")
	shared().requireSuccess(t, "Shared: no")
	deploy("--shared").requireSuccess(t, "NOT_SHARED_SMOKE (saved, shared)")
	shared().requireSuccess(t, "Shared: yes")

	// The manifest's per-variable field writes the same membership, and the
	// pull carries it as the shared_variables list, without the name.
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "volcano", "volcano-config.yaml"), `version: 1
variables:
  - name: NOT_SHARED_SMOKE
    value: fake-value
    shared: false
`)
	// --shared just shared it, so the manifest changes sharing alone: the dry
	// run counts that as an update and leaves the variable shared.
	env.runCloudCLI(t, "config", "deploy", "--dry-run").requireSuccess(t, "Dry run: projected actions, nothing was applied.", "variables: 1 updated")
	shared().requireSuccess(t, "Shared: yes")
	env.runCloudCLI(t, "config", "deploy").requireSuccess(t, "Configuration deployed from")
	shared().requireSuccess(t, "Shared: no")
	env.runCloudCLI(t, "config", "pull", "--force").requireSuccess(t, "Configuration written to")
	pulled, err := os.ReadFile(filepath.Join(env.projectDir, "volcano", "volcano-config.yaml"))
	if err != nil {
		t.Fatalf("read pulled manifest: %v", err)
	}
	if !strings.Contains(string(pulled), "shared_variables:") || strings.Contains(string(pulled), "NOT_SHARED_SMOKE") {
		t.Fatalf("pulled manifest should list shared names without NOT_SHARED_SMOKE:\n%s", pulled)
	}
	env.runCloudCLI(t, "config", "deploy").requireSuccess(t, "Configuration deployed from")
	shared().requireSuccess(t, "Shared: no")

	// shared: true shares it again: one update in the dry run, which changes
	// nothing, then applied by the deploy.
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "volcano", "volcano-config.yaml"), `version: 1
variables:
  - name: NOT_SHARED_SMOKE
    value: fake-value
    shared: true
`)
	env.runCloudCLI(t, "config", "deploy", "--dry-run").requireSuccess(t, "Dry run: projected actions, nothing was applied.", "variables: 1 updated")
	shared().requireSuccess(t, "Shared: no")
	env.runCloudCLI(t, "config", "deploy").requireSuccess(t, "Configuration deployed from")
	shared().requireSuccess(t, "Shared: yes")

	env.runCloudCLI(t, "variables", "delete", "NOT_SHARED_SMOKE", "--yes").requireSuccess(t, "deleted")
}
