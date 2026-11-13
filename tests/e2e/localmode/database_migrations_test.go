package localmode

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

var localModeE2EMigrationRunPattern = regexp.MustCompile(`Run ([0-9a-f-]{36}) queued`)

// TestLocalModeE2EDatabaseMigrations is the cloud tracked-run round trip on
// the local stack's app database. Local mode has no branches or backups, so a
// run applies straight to the database, warns that it skipped both, and a
// failing file fails in the apply phase. It has its own stack, so a failure
// here does not stop the smoke's later checks.
func TestLocalModeE2EDatabaseMigrations(t *testing.T) {
	if os.Getenv("VOLCANO_LOCALMODE_E2E") != "1" {
		t.Skip("set VOLCANO_LOCALMODE_E2E=1 to run disposable Docker acceptance")
	}
	requireDocker(t)
	binary := buildLocalModeE2EBinary(t)
	env, dir := localModeE2EEnv(t), localModeE2EProjectDir(t)
	cleanup := func() {
		_, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "stop", "--clean")
	}
	cleanup()
	t.Cleanup(cleanup)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "start"),
		"Volcano is ready for local development.")

	migrations := filepath.Join("volcano", "migrations")
	writeLocalModeE2EFile(t, dir, filepath.Join(migrations, "001_create_items.sql"),
		"CREATE TABLE items (id bigint PRIMARY KEY);\nCREATE INDEX items_created_idx ON items (id);\n")
	status := runVolcanoLocalModeE2E(t, binary, env, dir, "databases", "migration", "status", "-d", "app")
	requireContains(t, status, "001_create_items.sql")
	requireContains(t, status, "pending")
	requireContains(t, status, "volcano databases migration apply -d app")

	applied := runVolcanoLocalModeE2E(t, binary, env, dir, "databases", "migration", "apply", "-d", "app")
	requireContains(t, applied, "succeeded: 1 applied, 0 already applied")
	requireContains(t, applied, "rehearsal_skipped")
	requireContains(t, applied, "backup_skipped")
	match := localModeE2EMigrationRunPattern.FindStringSubmatch(applied)
	if match == nil {
		t.Fatalf("apply did not print its run ID:\n%s", applied)
	}
	runID := match[1]

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "databases", "migration", "apply", "-d", "app"),
		"succeeded: 0 applied, 1 already applied")

	writeLocalModeE2EFile(t, dir, filepath.Join(migrations, "002_create_orders.sql"), "CREATE TABLE orders (id bigint PRIMARY KEY);\n")
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"databases", "migration", "baseline", "-d", "app", "-f", "002_create_orders", "--yes"),
		"Recorded 1 file(s) as applied on database 'app', without running them")
	status = runVolcanoLocalModeE2E(t, binary, env, dir, "databases", "migration", "status", "-d", "app")
	requireContains(t, status, "applied")
	requireContains(t, status, "baselined")

	writeLocalModeE2EFile(t, dir, filepath.Join(migrations, "003_add_items_name.sql"), "ALTER TABLE items ADD COLUMN name text;\n")
	deployed := runVolcanoLocalModeE2EDeploy(t, binary, env, dir, "deploy", "--with-migrations")
	requireContains(t, deployed, "2. migrations  volcano/migrations -> database app")
	requireContains(t, deployed, "succeeded: 1 applied, 2 already applied")
	requireContains(t, deployed, "003_add_items_name.sql")
	requireContains(t, deployed, "Deployed in")

	broken := filepath.Join(migrations, "004_broken.sql")
	writeLocalModeE2EFile(t, dir, broken, "ALTER TABLE missing_table ADD COLUMN name text;\n")
	failed, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "databases", "migration", "apply", "-d", "app")
	if err == nil {
		t.Fatalf("a run with a failing file has to fail\n%s", failed)
	}
	requireContains(t, failed, "failed in apply (migration_failed)")
	requireContains(t, failed, `relation "missing_table" does not exist`)
	removeLocalModeE2EFile(t, dir, broken)

	runs := runVolcanoLocalModeE2E(t, binary, env, dir, "databases", "migration", "runs", "list", "-d", "app")
	requireContains(t, runs, runID)
	requireContains(t, runs, "migration_failed")
	requireContains(t, runs, "Showing 4 migration run(s)")
	run := runVolcanoLocalModeE2E(t, binary, env, dir, "databases", "migration", "runs", "get", runID, "-d", "app")
	requireContains(t, run, "Status: succeeded")
	requireContains(t, run, "Phase: apply")
	requireContains(t, run, "001_create_items.sql")
	requireContains(t, run, "rehearsal_skipped")
}
