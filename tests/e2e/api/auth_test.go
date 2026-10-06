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
