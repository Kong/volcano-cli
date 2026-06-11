package api

import "testing"

func TestAPIE2ESmokeMigrations(t *testing.T) {
	env := setupAPIE2E(t, "smoke-migrations")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	migrationResult := env.runCloudCLI(t, "databases", "migration", "up", "--all", "-d", "missing_database")
	migrationResult.requireFailure(t, "missing_database")
}
