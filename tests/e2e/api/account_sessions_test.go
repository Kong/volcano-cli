package api

import "testing"

// TestAPIE2ESmokeAccountSessions drives `volcano account sessions` against a
// deployed server. A user made through the management API has signed in nowhere,
// so the list is empty and signing out the others reaches nothing; a session of
// its own needs a device login, which the lane cannot complete.
func TestAPIE2ESmokeAccountSessions(t *testing.T) {
	env := setupAPIE2E(t, "smoke-account-sessions")
	env.loginAndUse(t)

	env.runCLI(t, "account", "sessions", "list").requireSuccess(t, "No sessions")
	env.runCLI(t, "account", "sessions", "list", "--json").requireSuccess(t, `"sessions"`)

	// The token login has no session of its own, so the command says every
	// session ends, and that tokens like this one keep working.
	env.runCLI(t, "account", "sessions", "revoke", "--all-others", "--yes").
		requireSuccess(t, "Signed out every session; this CLI stays logged in", "Tokens not tied to a session")
	env.runCLI(t, "projects").requireSuccess(t, env.projectID)

	const missing = "00000000-0000-4000-8000-000000000000"
	env.runCLI(t, "account", "sessions", "revoke", missing).requireFailure(t, "no session with that ID on this account: "+missing)
	env.runCLI(t, "account", "sessions", "revoke", "not-an-id").requireFailure(t, `"not-an-id" is not a session ID`)
}
