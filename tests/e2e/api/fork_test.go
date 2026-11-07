package api

import "testing"

// TestAPIE2EFork checks that a share link nobody issued is refused with the
// unavailable message and creates nothing, through volcano fork and through
// projects create --from. Forking a real share needs a published release,
// which needs a project with deployed source that the harness does not set up
// here. The same message answers a link that was switched off, has no release,
// or comes from a copy whose source stopped re-sharing. Where the gate is
// closed, the refusal says the feature is off instead.
func TestAPIE2EFork(t *testing.T) {
	env := setupAPIE2E(t, "fork")
	env.loginAndUse(t)

	const unknownID = "aaaaaaaaaaaa"
	const name = "cli-e2e-fork-never-created"
	refusal := "This share link is not available."
	if !sharesAvailable(t, env) {
		refusal = sharingOffMessage
	}
	env.runCLI(t, "fork", "https://volcano.dev/deploy/"+unknownID, name, "--yes").requireFailure(t, refusal)
	env.runCLI(t, "fork", unknownID, "--yes", "--no-wait").requireFailure(t, refusal)
	env.runCLI(t, "projects", "create", name, "--from", unknownID, "--yes").requireFailure(t, refusal)

	// A malformed link is refused before anything is sent.
	env.runCLI(t, "fork", "https://volcano.dev/projects/"+unknownID, "--yes").requireFailure(t, "invalid share link")
	env.runCLI(t, "projects", "create", name, "--from", unknownID, "--template", "trellini").
		requireFailure(t, "none of the others can be")
	env.runCLI(t, "fork", unknownID, "--var", "NO_EQUALS", "--yes").requireFailure(t, "each one must be NAME=value")

	listed := env.runCLI(t, "projects", "list")
	listed.requireSuccess(t)
	listed.requireNotContains(t, name)
}
