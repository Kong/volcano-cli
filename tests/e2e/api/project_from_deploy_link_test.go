package api

import "testing"

// TestAPIE2ECloudProjectFromDeployLink checks what an account with no GitHub
// connection can: that a deploy link nobody issued is refused with the
// unavailable message and creates nothing. Creating a project from a real link
// needs a public repository its owner can push to, which the harness cannot
// set up. The same message answers a link switched off and a deployment whose
// deploy links are not available, so this holds in either environment.
func TestAPIE2ECloudProjectFromDeployLink(t *testing.T) {
	env := setupAPIE2E(t, "cloud-from-link")
	env.loginAndUse(t)

	const unknownID = "aaaaaaaaaaaa"
	const name = "cli-e2e-from-link-never-created"
	env.runCLI(t, "projects", "create", name, "--from", "https://volcano.dev/deploy/"+unknownID, "--yes").
		requireFailure(t, "This deploy link is not available.")
	env.runCLI(t, "projects", "create", name, "--from", unknownID, "--yes", "--no-wait").
		requireFailure(t, "This deploy link is not available.")

	// A malformed link is refused before anything is sent.
	env.runCLI(t, "projects", "create", name, "--from", "https://volcano.dev/projects/"+unknownID, "--yes").
		requireFailure(t, "invalid deploy link")
	env.runCLI(t, "projects", "create", name, "--from", unknownID, "--template", "trellini").
		requireFailure(t, "none of the others can be")

	listed := env.runCLI(t, "projects", "list")
	listed.requireSuccess(t)
	listed.requireNotContains(t, name)
}
