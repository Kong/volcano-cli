package api

import (
	"path/filepath"
	"testing"
)

func TestAPIE2ESmokeVariables(t *testing.T) {
	env := setupAPIE2E(t, "smoke-variables")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCLI(t, "variables", "deploy").requireSuccess(t, "SMOKE_MESSAGE", "variable(s) saved")
	env.runCLI(t, "variables", "list").requireSuccess(t, "SMOKE_MESSAGE")
	env.runCLI(t, "variables", "get", "SMOKE_MESSAGE").requireSuccess(t, "SMOKE_MESSAGE")

	secondaryProject := "cli-e2e-smoke-secondary-" + apiE2ESuffix(t)
	secondaryProjectID := createAPIE2EProject(t, env.apiURL, env.token, secondaryProject)
	t.Cleanup(func() {
		deleteAPIE2EProject(env.apiURL, env.token, secondaryProjectID)
	})
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "secondary.env"), "SECOND_PROJECT_ONLY=1\n")
	env.runCLI(t, "use", secondaryProject).requireSuccess(t, "Now using project")
	env.runCLI(t, "variables", "deploy", "--file", "secondary.env").requireSuccess(t, "SECOND_PROJECT_ONLY", "variable(s) saved")
	env.runCLI(t, "variables", "list").requireSuccess(t, "SECOND_PROJECT_ONLY")
	env.runCLI(t, "use", env.projectID).requireSuccess(t, "Now using project")
	baseVariables := env.runCLI(t, "variables", "list")
	baseVariables.requireSuccess(t, "SMOKE_MESSAGE")
	baseVariables.requireNotContains(t, "SECOND_PROJECT_ONLY")

	env.runCLI(t, "variables", "delete", "SMOKE_MESSAGE", "--yes").requireSuccess(t, "deleted")
}
