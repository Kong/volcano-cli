package api

import (
	"regexp"
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

	// The singular alias is part of the advertised surface, so it has to reach the
	// same routes.
	env.runCloudCLI(t, "databases", "branch", "list", database).requireSuccess(t, branch)

	// A branch name is unique within its parent.
	env.runCloudCLI(t, "databases", "branches", "create", database, branch).requireFailure(t, branch)

	// A name that exists nowhere reads as missing rather than as a failure of the
	// command, on the branch and on the database it would belong to.
	env.runCloudCLI(t, "databases", "branches", "get", database, "cli_e2e_absent").
		requireFailure(t, "not found")
	env.runCloudCLI(t, "databases", "branches", "delete", database, "cli_e2e_absent", "--yes").
		requireFailure(t, "not found")
	env.runCloudCLI(t, "databases", "branches", "list", "cli_e2e_absent_db").
		requireFailure(t, "not found")

	// Left off, the lifetime is the platform's default rather than nothing: a
	// branch with no expiry would be one nothing ever collects. This is also the
	// only path that proves the CLI omits the field instead of sending a zero.
	defaulted := "cli_e2e_default_ttl"
	env.runCloudCLI(t, "databases", "branches", "create", database, defaulted).
		requireSuccess(t, "Branch '"+defaulted+"' of database '"+database+"' created")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "databases", "branches", "delete", database, defaulted, "--yes")
	})
	env.runCloudCLI(t, "databases", "branches", "get", database, defaulted).requireSuccess(t, "(7d)")
	env.runCloudCLI(t, "databases", "branches", "delete", database, defaulted, "--yes").
		requireSuccess(t, "Branch '"+defaulted+"' of database '"+database+"' deleted")

	env.runCloudCLI(t, "databases", "branches", "extend", database, branch, "--ttl", "6h").
		requireSuccess(t, "Branch '"+branch+"' now expires")
	env.runCloudCLI(t, "databases", "branches", "reset", database, branch, "--yes").
		requireSuccess(t, "Branch '"+branch+"' reset to database '"+database+"'")

	// The reset only claims the branch: it returns with the branch already out of
	// service in 'provisioning' and rewinds in the background. Rotating a password
	// needs an active branch, so wait for the rewind to land before going on.
	env.waitForCloudCLIContains(t, 15*time.Minute, "Status: active",
		"databases", "branches", "get", database, branch)

	// Rotating a branch's password is the branch's business. The parent shares the
	// branch's data, not its credentials, and an owner rotating a throwaway branch
	// must not find the parent's connection string invalidated underneath them.
	parentBefore := apiE2EConnectionString(t, env.runCloudCLI(t, "databases", "get", database, "--show-connection-string").output)
	branchBefore := apiE2EConnectionString(t,
		env.runCloudCLI(t, "databases", "branches", "get", database, branch, "--show-connection-string").output)

	rotated := env.runCloudCLI(t, "databases", "branches", "rotate-password", database, branch,
		"--yes", "--show-connection-string")
	rotated.requireSuccess(t, "Password rotated for branch '"+branch+"'", "postgresql://")
	printed := apiE2EConnectionString(t, rotated.output)

	parentAfter := apiE2EConnectionString(t, env.runCloudCLI(t, "databases", "get", database, "--show-connection-string").output)
	branchAfter := apiE2EConnectionString(t,
		env.runCloudCLI(t, "databases", "branches", "get", database, branch, "--show-connection-string").output)
	// The credential the rotation printed is the one it installed, not a
	// pre-rotation read: an owner who only keeps what the command printed has to
	// end up with a working branch.
	if printed != branchAfter {
		t.Fatalf("rotate-password printed %q but the branch now serves %q", printed, branchAfter)
	}
	if parentAfter != parentBefore {
		t.Fatalf("rotating branch %q changed the parent's connection string", branch)
	}
	if branchAfter == branchBefore {
		t.Fatalf("rotating branch %q left its own connection string unchanged", branch)
	}

	env.runCloudCLI(t, "databases", "branches", "delete", database, branch, "--yes").
		requireSuccess(t, "Branch '"+branch+"' of database '"+database+"' deleted")

	// Deleting a branch leaves its parent alone.
	env.runCloudCLI(t, "databases", "get", database).requireSuccess(t, "Name: "+database, "Status: active")
}

var apiE2EConnectionStringLine = regexp.MustCompile(`(?m)^Connection string:\s+(\S+)`)

// apiE2EConnectionString pulls the connection string out of a get, which is the
// only place the caller gets one.
func apiE2EConnectionString(t *testing.T, output string) string {
	t.Helper()
	match := apiE2EConnectionStringLine.FindStringSubmatch(output)
	if match == nil {
		t.Fatalf("output carried no connection string:\n%s", output)
	}
	return match[1]
}
