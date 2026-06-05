package api

import "testing"

func TestAPIE2ESmokeProjects(t *testing.T) {
	env := setupAPIE2E(t, "smoke-projects")

	env.loginAndUse(t)
	env.runCLI(t, "projects").requireSuccess(t, env.projectID)
	env.runCLI(t, "projects", "get", env.projectID).requireSuccess(t, env.project)
	env.runCLI(t, "use", "missing-"+apiE2ESuffix(t)).requireFailure(t, "project not found")
}
