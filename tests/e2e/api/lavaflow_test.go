package api

import "testing"

// TestAPIE2ECloudLavaFlow turns LavaFlow on and off for a fresh project the way
// a user does, and checks the plan gate refuses it first. A new project has no
// issues, so no run starts; Kong/volcano-hosting's cloud E2E covers runs.
func TestAPIE2ECloudLavaFlow(t *testing.T) {
	env := setupAPIE2E(t, "cloud-lavaflow")
	env.loginAndUse(t)

	env.runCloudCLI(t, "lavaflow", "enable").requireFailure(t,
		"failed to turn LavaFlow on",
		"LavaFlow needs a paid plan",
	)

	env.setUserPlan(t, "SUPERAGENT")
	off := env.runCloudCLI(t, "lavaflow", "status")
	off.requireSuccess(t, "LavaFlow: off", "Plan: allows LavaFlow", "Repository: not connected",
		"Turn it on: volcano cloud lavaflow enable")

	env.runCloudCLI(t, "lavaflow", "enable").requireSuccess(t, "LavaFlow is on", "usage-billed add-on")
	on := env.runCloudCLI(t, "lavaflow", "status")
	on.requireSuccess(t, "LavaFlow: on", "Connect a repository so LavaFlow can open fix pull requests")
	on.requireNotContains(t, "max_stage", "auto_trigger")

	env.runCloudCLI(t, "lavaflow", "runs", "list").requireSuccess(t, "No LavaFlow runs")

	env.runCloudCLI(t, "lavaflow", "disable").requireSuccess(t, "LavaFlow is off")
	env.runCloudCLI(t, "lavaflow", "status").requireSuccess(t, "LavaFlow: off")
}
