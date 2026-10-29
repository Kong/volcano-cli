package api

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

var apiE2EMigrationRunPattern = regexp.MustCompile(`Run ([0-9a-f-]{36}) queued`)

func TestAPIE2ESmokeMigrations(t *testing.T) {
	env := setupAPIE2E(t, "smoke-migrations")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	migrationResult := env.runCloudCLI(t, "databases", "migration", "up", "--all", "-d", "missing_database")
	migrationResult.requireFailure(t, "missing_database")
}

// TestAPIE2ECloudDatabaseMigrations drives tracked migration runs against a
// real database: a run that rehearses on a branch before it applies, a run
// with nothing pending, a baseline, a --with-migrations deploy, and a file
// that fails in the rehearsal. The refusals that need no database live in the
// hosting repo's CLI suite.
func TestAPIE2ECloudDatabaseMigrations(t *testing.T) {
	env := setupAPIE2E(t, "cloud-database-migrations")

	env.loginAndUse(t)
	database := "cli_e2e_" + apiE2ESuffix(t)
	env.runCloudCLI(t, "databases", "create", database, "--region", apiE2EDefaultRegion, "--pg-version", apiE2EDefaultPGVersion).requireSuccess(t, database)
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "databases", "delete", database, "--yes")
	})
	env.waitForDatabaseActive(t, database, 30*time.Minute)

	migrations := filepath.Join(env.projectDir, "volcano", "migrations")
	// A tracked run takes several statements in one file, which an untracked
	// one refuses.
	writeAPIE2EFile(t, filepath.Join(migrations, "001_create_items.sql"),
		"CREATE TABLE items (id bigint PRIMARY KEY);\nCREATE INDEX items_created_idx ON items (id);\n")
	env.runCloudCLI(t, "databases", "migration", "status", "-d", database).requireSuccess(t,
		"001_create_items.sql", "pending", "If they already ran without tracking, record them instead with")

	// A new user is on HOBBY: the run rehearses on a branch but takes no backup.
	applied := env.runCloudCLI(t, "databases", "migration", "apply", "-d", database)
	applied.requireSuccess(t, "succeeded: 1 applied, 0 already applied", "001_create_items.sql", "backup_skipped")
	applied.requireNotContains(t, "rehearsal_skipped")
	match := apiE2EMigrationRunPattern.FindStringSubmatch(applied.output)
	if match == nil {
		t.Fatalf("apply did not print its run ID:\n%s", applied.output)
	}
	runID := match[1]

	env.runCloudCLI(t, "databases", "migration", "apply", "-d", database).
		requireSuccess(t, "succeeded: 0 applied, 1 already applied")

	// A file the database already reflects is recorded, not run.
	writeAPIE2EFile(t, filepath.Join(migrations, "002_create_orders.sql"), "CREATE TABLE orders (id bigint PRIMARY KEY);\n")
	env.runCloudCLI(t, "databases", "migration", "baseline", "-d", database, "-f", "002_create_orders", "--yes").
		requireSuccess(t, "Recorded 1 file(s) as applied on database '"+database+"', without running them", "002_create_orders.sql")
	env.runCloudCLI(t, "databases", "migration", "baseline", "-d", database, "--all", "--yes").
		requireSuccess(t, "Nothing to record")
	env.runCloudCLI(t, "databases", "migration", "status", "-d", database).
		requireSuccess(t, "001_create_items.sql", "applied", "002_create_orders.sql", "baselined")

	// --with-migrations applies in a tracked run too, so only the new file
	// runs. With nothing else in the directory, every other step is skipped.
	writeAPIE2EFile(t, filepath.Join(migrations, "003_add_items_name.sql"), "ALTER TABLE items ADD COLUMN name text;\n")
	env.runCloudCLI(t, "deploy", "--with-migrations", "-d", database).requireSuccess(t,
		"2. migrations  volcano/migrations -> database "+database,
		"succeeded: 1 applied, 2 already applied",
		"003_add_items_name.sql",
		"Deployed in")

	// A file that fails stops the run in the rehearsal, before the database.
	broken := filepath.Join(migrations, "004_broken.sql")
	writeAPIE2EFile(t, broken, "ALTER TABLE missing_table ADD COLUMN name text;\n")
	env.runCloudCLI(t, "databases", "migration", "apply", "-d", database).requireFailure(t,
		"failed in rehearse (migration_failed)",
		`relation "missing_table" does not exist`,
		"production was not changed")
	if err := os.Remove(broken); err != nil {
		t.Fatalf("remove %s: %v", broken, err)
	}
	env.runCloudCLI(t, "databases", "migration", "status", "-d", database).
		requireSuccess(t, "003_add_items_name.sql", "applied")

	env.runCloudCLI(t, "databases", "migration", "runs", "list", "-d", database).
		requireSuccess(t, runID, "succeeded", "failed", "migration_failed", "Showing 4 migration run(s)")
	env.runCloudCLI(t, "databases", "migration", "runs", "get", runID, "-d", database).
		requireSuccess(t, "Status: succeeded", "Phase: apply", "001_create_items.sql", "applied")
}
