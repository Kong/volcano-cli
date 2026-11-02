package api

import "testing"

func TestAPIE2ESmokeAuth(t *testing.T) {
	// Any token the platform has never issued; login must reject it.
	const invalidToken = "invalid-token"
	env := setupAPIE2E(t, "smoke-auth")

	env.runCLI(t, "projects").requireFailure(t, "not authenticated")
	env.runCLI(t, "login", "--token", invalidToken).requireFailure(t, "invalid token")
	env.runCLIWithEnv(t, []string{"VOLCANO_TOKEN=" + env.token}, "projects").requireSuccess(t, env.projectID)
	env.runCLI(t, "logout").requireSuccess(t, "Logged out")
	env.runCLI(t, "login", "--token", env.token).requireSuccess(t, "Logged in successfully")
}

// TestAPIE2ESmokeLogoutRevokesTheLogin logs in with a token of its own, so
// revoking it leaves the one cleanup uses, and checks that after logout the
// server refuses the token everywhere it was copied.
func TestAPIE2ESmokeLogoutRevokesTheLogin(t *testing.T) {
	env := setupAPIE2E(t, "smoke-logout")
	token := createAPIE2ENamedUserToken(t, env.mgmtURL, env.userID, "cli-e2e-logout")

	env.runCLI(t, "login", "--token", token).requireSuccess(t, "Logged in successfully")
	env.runCLI(t, "logout").requireSuccess(t, "Logged out: this login's token and session were revoked on the server")
	env.runCLI(t, "login", "--token", token).requireFailure(t, "invalid token")

	// Only this login ended: the account's other token still works.
	env.runCLIWithEnv(t, []string{"VOLCANO_TOKEN=" + env.token}, "projects").requireSuccess(t, env.projectID)
}
