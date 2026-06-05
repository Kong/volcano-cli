package api

import (
	"testing"
	"time"
)

func TestAPIE2ECloudDatabases(t *testing.T) {
	env := setupAPIE2E(t, "cloud-databases")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	database := "cli_e2e_" + apiE2ESuffix(t)
	env.runCLI(t, "databases", "create", database, "--region", apiE2EDefaultRegion, "--pg-version", apiE2EDefaultPGVersion).requireSuccess(t, database)
	t.Cleanup(func() {
		_ = env.runCLI(t, "databases", "delete", database, "--yes")
	})
	env.waitForDatabaseActive(t, database, 30*time.Minute)
	env.runCLI(t, "databases", "list", "--show-connection-string").requireSuccess(t, database, "Connection string:")
	env.runCLI(t, "databases", "get", database, "--show-connection-string").requireSuccess(t, "Name: "+database, "Status: active", "Connection string:")
	env.runCLI(t, "databases", "migration", "up", "--all", "-d", database).requireSuccess(t, "Migrations deployed")
	env.runCLI(t, "databases", "delete", database, "--yes").requireSuccess(t, "deleted")
}
