package api

import (
	"testing"
	"time"
)

func TestAPIE2ECloudDatabaseBranches(t *testing.T) {
	env := setupAPIE2E(t, "cloud-database-branches")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	database := "cli_e2e_" + apiE2ESuffix(t)
	env.runCloudCLI(t, "databases", "create", database, "--region", apiE2EDefaultRegion, "--pg-version", apiE2EDefaultPGVersion).requireSuccess(t, database)
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "databases", "delete", database, "--yes")
	})
	env.waitForDatabaseActive(t, database, 30*time.Minute)

	// A branch name is a Postgres-safe identifier: lowercase alphanumeric and
	// underscores only, so no hyphens.
	branch := "cli_e2e_branch"
	env.runCloudCLI(t, "databases", "branches", "create", database, branch, "--ttl", "2h").
		requireSuccess(t, "Branch '"+branch+"' of database '"+database+"' created", "provisioning")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "databases", "branches", "delete", database, branch, "--yes")
	})

	active := env.waitForCloudCLIContains(t, 15*time.Minute, "Status: active",
		"databases", "branches", "get", database, branch)
	active.requireNotContains(t, "postgresql://")

	env.runCloudCLI(t, "databases", "branches", "get", database, branch, "--show-connection-string").
		requireSuccess(t, "Name: "+branch, "Status: active", "Connection string:", "postgresql://")
	env.runCloudCLI(t, "databases", "branches", "list", database).requireSuccess(t, branch, "active")

	// A branch name is unique within its parent.
	env.runCloudCLI(t, "databases", "branches", "create", database, branch).requireFailure(t, branch)

	env.runCloudCLI(t, "databases", "branches", "extend", database, branch, "--ttl", "6h").
		requireSuccess(t, "Branch '"+branch+"' now expires")
	env.runCloudCLI(t, "databases", "branches", "reset", database, branch, "--yes").
		requireSuccess(t, "Branch '"+branch+"' reset to database '"+database+"'")
	env.runCloudCLI(t, "databases", "branches", "rotate-password", database, branch, "--yes").
		requireSuccess(t, "Password rotated for branch '"+branch+"'")

	env.runCloudCLI(t, "databases", "branches", "delete", database, branch, "--yes").
		requireSuccess(t, "Branch '"+branch+"' of database '"+database+"' deleted")

	// Deleting a branch leaves its parent alone.
	env.runCloudCLI(t, "databases", "get", database).requireSuccess(t, "Name: "+database, "Status: active")
}
