package api

import (
	"strings"
	"testing"
)

// lavaflowUnavailable is what every LavaFlow command says where Volcano has
// LavaFlow off.
const lavaflowUnavailable = "LavaFlow isn't available on this Volcano API"

// TestAPIE2ECloudLavaFlow turns LavaFlow on and off for a fresh project the way
// a user does, and checks the plan gate refuses it first. The project has no
// issues and no connected repository, so no run starts and nothing can open or
// merge a pull request; Kong/volcano-hosting's cloud E2E covers runs.
//
// Volcano turns LavaFlow on per environment, and where it is off every command
// says so: then that is all there is to check, as hosting's own LavaFlow
// settings scenario does in the same environments.
func TestAPIE2ECloudLavaFlow(t *testing.T) {
	env := setupAPIE2E(t, "cloud-lavaflow")
	env.loginAndUse(t)
	env.runCLI(t, "git", "status").requireSuccess(t, "No repository is connected")

	if probe := env.runCloudCLI(t, "lavaflow", "status"); strings.Contains(probe.output, lavaflowUnavailable) {
		probe.requireFailure(t, lavaflowUnavailable)
		for _, command := range [][]string{{"lavaflow", "enable"}, {"lavaflow", "runs", "list"}} {
			env.runCloudCLI(t, command...).requireFailure(t, lavaflowUnavailable)
		}
		t.Log("LavaFlow is off in this environment, so only its off response is checked")
		return
	}

	// Registered after the project's own cleanup, so it runs first: LavaFlow is
	// off before the project is deleted, even when an assertion below fails.
	t.Cleanup(func() { env.runCloudCLI(t, "lavaflow", "disable").requireSuccess(t, "LavaFlow is off") })

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
