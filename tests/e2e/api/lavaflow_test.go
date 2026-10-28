package api

import (
	"strings"
	"testing"
)

// lavaflowUnavailable is what a LavaFlow command says where Volcano has
// LavaFlow off and the command needs it on, or saved settings the project
// does not have.
const lavaflowUnavailable = "LavaFlow isn't available on this Volcano API"

// TestAPIE2ECloudLavaFlow turns LavaFlow on and off for a fresh project the way
// a user does, and checks the plan gate refuses it first. The project has no
// issues and no connected repository, so no run starts and nothing can open or
// merge a pull request; Kong/volcano-hosting's cloud E2E covers runs.
//
// Volcano turns LavaFlow on per environment. Where it is off, a fresh project
// has no saved settings, so status, enable and disable say LavaFlow isn't
// available, while its runs stay readable.
func TestAPIE2ECloudLavaFlow(t *testing.T) {
	env := setupAPIE2E(t, "cloud-lavaflow")
	env.loginAndUse(t)
	env.runCLI(t, "git", "status").requireSuccess(t, "No repository is connected")

	// TODO(lavaflow): Platform is turning LavaFlow on in the cloud-e2e
	// environment. Once it is on there, remove this branch so the round trip
	// below is required rather than skipped.
	if probe := env.runCloudCLI(t, "lavaflow", "status"); strings.Contains(probe.output, lavaflowUnavailable) {
		probe.requireFailure(t, lavaflowUnavailable)
		for _, command := range [][]string{{"lavaflow", "enable"}, {"lavaflow", "disable"}} {
			env.runCloudCLI(t, command...).requireFailure(t, lavaflowUnavailable)
		}
		env.runCloudCLI(t, "lavaflow", "runs", "list").requireSuccess(t, "No LavaFlow runs")
		t.Log("LavaFlow is off in this environment, so only its off responses are checked")
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

	env.runCloudCLI(t, "lavaflow", "enable").requireSuccess(t, "LavaFlow is on",
		"Until you connect a repository, it stops at the diagnosis. Connecting one pauses LavaFlow until you "+
			"turn it on again: volcano git connect, then volcano cloud lavaflow enable",
		"During the preview, it needs the SUPERAGENT plan")
	on := env.runCloudCLI(t, "lavaflow", "status")
	on.requireSuccess(t, "LavaFlow: on, diagnosing only until a repository is connected",
		"Connect a repository, then turn LavaFlow on again so it can open fix pull requests: "+
			"volcano git connect, then volcano cloud lavaflow enable")
	on.requireNotContains(t, "max_stage", "auto_trigger")

	env.runCloudCLI(t, "lavaflow", "runs", "list").requireSuccess(t, "No LavaFlow runs")

	env.runCloudCLI(t, "lavaflow", "disable").requireSuccess(t, "LavaFlow is off")
	env.runCloudCLI(t, "lavaflow", "status").requireSuccess(t, "LavaFlow: off")
}
