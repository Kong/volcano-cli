package migration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
)

func setTrackedMigrationTest(t *testing.T) {
	t.Helper()
	setMigrationCommandTestHome(t)
	t.Chdir(t.TempDir())
	saveMigrationCommandTestConfig(t)
}

func TestApplySendsEveryFileAndWaitsForTheRun(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "002_add_pinned.sql", "ALTER TABLE notes ADD COLUMN pinned boolean;")
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);\nCREATE INDEX ON notes (id);")
	fake := newFakeMigrationAPI(t)
	fake.runs = []map[string]any{
		migrationRun("running", "rehearse", nil),
		migrationRun("running", "apply", nil),
		migrationRun("succeeded", "apply", []string{"001_create_notes.sql:already_applied", "002_add_pinned.sql:applied"},
			map[string]any{
				"backup_name": "before-migration-" + migrationRunID,
				"warnings":    []any{map[string]any{"code": "out_of_order", "file": "002_add_pinned.sql", "message": "002_add_pinned.sql sorts before 003"}},
			}),
	}
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app")
	require.NoError(t, err, out)

	assert.Equal(t, []string{"start"}, fake.recorded())
	assert.Equal(t, [][]string{{"001_create_notes.sql", "002_add_pinned.sql"}}, fake.sentNames())
	assert.Equal(t, "CREATE TABLE notes (id int);\nCREATE INDEX ON notes (id);", fake.sent[0].Files[0].SQL)
	require.Len(t, fake.keys, 1)
	assert.True(t, strings.HasPrefix(fake.keys[0], "volcano-cli-"), fake.keys[0])
	for _, want := range []string{
		"Starting a migration run of 2 file(s) from volcano/migrations on database 'app'",
		"Run " + migrationRunID + " queued",
		"running: rehearse",
		"running: apply",
		"succeeded: apply",
		"Migration run " + migrationRunID + " succeeded: 1 applied, 1 already applied",
		"Backup taken first: before-migration-" + migrationRunID,
		"002_add_pinned.sql",
		"Warning: out_of_order: 002_add_pinned.sql sorts before 003",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "001_create_notes.sql ", "a file that was already applied is counted, not listed")
}

func TestApplySendsTheDatabasesDirectoryWithItsName(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "app/001_create_notes.sql", "CREATE TABLE notes (id int);")
	writeMigrationFile(t, "analytics/001_create_events.sql", "CREATE TABLE events (id int);")
	fake := newFakeMigrationAPI(t)
	fake.startBody = migrationRun("succeeded", "plan", []string{"001_create_notes.sql:already_applied"})
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app")
	require.NoError(t, err, out)

	assert.Equal(t, [][]string{{"app/001_create_notes.sql"}}, fake.sentNames())
	assert.Contains(t, out, "from volcano/migrations/app on database 'app'")
	assert.Contains(t, out, "succeeded: 0 applied, 1 already applied")
}

// Where the API does not offer tracked runs, apply says so and names the
// untracked command instead of sending anything.
func TestApplyRefusesWhereTrackedRunsAreUnavailable(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	fake := newFakeMigrationAPI(t)
	fake.unavailable = true
	server := fake.start()

	_, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app")
	require.EqualError(t, err, `tracked migration runs aren't available on this Volcano API (capability "databases.migrations"). `+
		`To run the files without tracking, which runs every file again, use 'volcano databases migration up --all -d app'`)
	assert.Empty(t, fake.recorded())
}

// The capability list can change between the read and the start; the start's
// own refusal reads the same and keeps its 404 for the exit code.
func TestApplyWordsAFeatureUnavailableRefusal(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	fake := newFakeMigrationAPI(t)
	fake.startStatus = http.StatusNotFound
	fake.startBody = map[string]any{"error": "migration runs are not available", "code": "feature_unavailable"}
	server := fake.start()

	_, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app")
	require.ErrorContains(t, err, "tracked migration runs aren't available on this Volcano API")
	assert.Equal(t, http.StatusNotFound, api.Status(err))
}

func TestApplyNamesTheRunInProgress(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	fake := newFakeMigrationAPI(t)
	fake.startStatus = http.StatusConflict
	fake.startBody = map[string]any{"error": "a migration run is already queued or running", "code": "migration_run_in_progress", "run_id": migrationRunID}
	server := fake.start()

	_, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app")
	require.ErrorContains(t, err, "a migration run is already queued or running")
	require.ErrorContains(t, err, "Wait for it to end with 'volcano databases migration runs get "+migrationRunID+" -d app', then run this command again")
	assert.Equal(t, http.StatusConflict, api.Status(err))
}

// A failed run lists what became of the files it did not settle, keeps the
// database's own message on the file rather than in the error, and suggests a
// baseline when nothing was ever recorded.
func TestApplyReportsAFailedRun(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	writeMigrationFile(t, "002_add_pinned.sql", "ALTER TABLE notes ADD COLUMN pinned boolean;")
	failed := migrationRun("failed", "rehearse", []string{"001_create_notes.sql:failed", "002_add_pinned.sql:not_run"}, map[string]any{
		"error": map[string]any{"code": "migration_failed", "message": "migration 001_create_notes.sql failed at line 1 (SQLSTATE 42P07); it failed in rehearsal, so production was not changed"},
	})
	files := failed["files"].([]any)
	files[0].(map[string]any)["error"] = map[string]any{"message": `relation "notes" already exists`, "sqlstate": "42P07", "line": 1}
	fake := newFakeMigrationAPI(t)
	fake.runs = []map[string]any{failed}
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app")
	require.Error(t, err)
	assert.Contains(t, out, "Migration run "+migrationRunID+" failed in rehearse: 0 applied, 0 already applied")
	assert.Contains(t, out, `relation "notes" already exists (SQLSTATE 42P07, line 1)`)
	assert.Contains(t, out, "not run")
	assert.Equal(t, "migration run "+migrationRunID+" failed in rehearse (migration_failed): "+
		"migration 001_create_notes.sql failed at line 1 (SQLSTATE 42P07); it failed in rehearsal, so production was not changed\n"+
		`If database "app" already reflects these files because they ran without tracking, `+
		"record them with 'volcano databases migration baseline --all -d app' instead", err.Error())
	assert.NotContains(t, err.Error(), "already exists", "the database's message stays out of the error")
}

func TestApplyNoWaitReturnsTheQueuedRun(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	fake := newFakeMigrationAPI(t)
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app", "--no-wait", "--json")
	require.NoError(t, err, out)

	var run map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &run), out)
	assert.Equal(t, "queued", run["status"])
	assert.Zero(t, fake.runReads)
}

func TestApplyWithoutFilesSendsNothing(t *testing.T) {
	setTrackedMigrationTest(t)
	fake := newFakeMigrationAPI(t)
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "No migration files for database 'app' in volcano/migrations; nothing to apply")
	assert.Zero(t, fake.capReads)
	assert.Empty(t, fake.recorded())
}

func TestStatusComparesTheFilesWithTheHistory(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "\uFEFFCREATE TABLE notes (id int);\r\n")
	writeMigrationFile(t, "002_add_pinned.sql", "ALTER TABLE notes ADD COLUMN pinned boolean; -- edited")
	writeMigrationFile(t, "003_add_tags.sql", "CREATE TABLE tags (id int);")
	fake := newFakeMigrationAPI(t)
	fake.history = map[string]any{
		"history": []any{
			historyEntry("000_old.sql", "sha256:00", "applied"),
			historyEntry("001_create_notes.sql", checksumOf("CREATE TABLE notes (id int);\n"), "applied"),
			historyEntry("002_add_pinned.sql", checksumOf("ALTER TABLE notes ADD COLUMN pinned boolean;"), "baselined"),
		},
		"active_run_id": migrationRunID,
	}
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "status", "-d", "app")
	require.NoError(t, err, out)
	for _, want := range []string{
		"000_old.sql", "missing",
		"001_create_notes.sql", "applied",
		"002_add_pinned.sql", "changed",
		"003_add_tags.sql", "pending",
		"1 applied, 1 pending, 1 changed, 1 missing",
		"Run " + migrationRunID + " is queued or running. Follow it with: volcano databases migration runs get " + migrationRunID + " -d app",
		"A run refuses an applied file whose content changed",
		"Apply the pending files with: volcano databases migration apply -d app",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "If they already ran without tracking")
	assert.Empty(t, fake.recorded())

	out, err = executeMigrationCommand(t, New(fake.deps(server)), "status", "-d", "app", "--json")
	require.NoError(t, err, out)
	var status struct {
		Database    string `json:"database"`
		ActiveRunID string `json:"active_run_id"`
		Files       []struct {
			Name  string `json:"name"`
			State string `json:"state"`
		} `json:"files"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &status), out)
	assert.Equal(t, "app", status.Database)
	assert.Equal(t, migrationRunID, status.ActiveRunID)
	var states []string
	for _, file := range status.Files {
		states = append(states, file.Name+"="+file.State)
	}
	assert.Equal(t, []string{"000_old.sql=missing", "001_create_notes.sql=applied", "002_add_pinned.sql=changed", "003_add_tags.sql=pending"}, states)
}

// An empty history with files to apply is the case of a database migrated
// without tracking, so status offers the baseline too.
func TestStatusOffersABaselineForAnUntrackedDatabase(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	fake := newFakeMigrationAPI(t)
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "status", "-d", "app")
	require.NoError(t, err, out)
	assert.Contains(t, out, "If they already ran without tracking, record them instead with: volcano databases migration baseline --all -d app")
}

func TestStatusWithoutFilesShowsTheHistory(t *testing.T) {
	setTrackedMigrationTest(t)
	fake := newFakeMigrationAPI(t)
	fake.history = map[string]any{"history": []any{historyEntry("001_create_notes.sql", "sha256:00", "applied")}}
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "status", "-d", "app")
	require.NoError(t, err, out)
	assert.Contains(t, out, "No migration files for database 'app' in volcano/migrations; showing its history only.")
	assert.Contains(t, out, "001_create_notes.sql")
	assert.NotContains(t, out, "missing")
}

func TestBaselineRecordsTheNamedFilesAfterConfirming(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	writeMigrationFile(t, "002_add_pinned.sql", "ALTER TABLE notes ADD COLUMN pinned boolean;")
	writeMigrationFile(t, "003_add_tags.sql", "CREATE TABLE tags (id int);")
	fake := newFakeMigrationAPI(t)
	fake.baselineBody = map[string]any{"recorded": []string{"001_create_notes.sql", "002_add_pinned.sql"}}
	server := fake.start()

	cmd := New(fake.deps(server))
	cmd.SetIn(bytes.NewBufferString("yes\n"))
	out, err := executeMigrationCommand(t, cmd, "baseline", "-d", "app", "-f", "001_create_notes", "-f", "volcano/migrations/002_add_pinned.sql")
	require.NoError(t, err, out)

	assert.Equal(t, [][]string{{"001_create_notes.sql", "002_add_pinned.sql"}}, fake.sentNames())
	assert.Contains(t, out, "A baseline records these files in the migration history of database 'app' as applied, without running them.")
	assert.Contains(t, out, "Record 2 file(s) as applied?")
	assert.Contains(t, out, "Recorded 2 file(s) as applied on database 'app', without running them")
}

func TestBaselineDeclinedRecordsNothing(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	fake := newFakeMigrationAPI(t)
	server := fake.start()

	cmd := New(fake.deps(server))
	cmd.SetIn(bytes.NewBufferString("no\n"))
	_, err := executeMigrationCommand(t, cmd, "baseline", "-d", "app", "--all")
	require.NoError(t, err)
	assert.Empty(t, fake.recorded())
}

func TestBaselineRefusesWhatItCannotRecord(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		setup  func(*fakeMigrationAPI)
		want   string
		status int
	}{
		{
			name: "needs all or file",
			args: []string{"baseline", "-d", "app", "--yes"},
			want: "at least one of the flags in the group [all file] is required",
		},
		{
			name: "unknown file",
			args: []string{"baseline", "-d", "app", "-f", "009_missing", "--yes"},
			want: "migration \"009_missing\" not found in volcano/migrations/\navailable migrations: 001_create_notes.sql",
		},
		{
			name:  "unavailable",
			args:  []string{"baseline", "-d", "app", "--all", "--yes"},
			setup: func(f *fakeMigrationAPI) { f.unavailable = true },
			want: `tracked migration runs aren't available on this Volcano API (capability "databases.migrations"), ` +
				"so there is no migration history to record a baseline in",
		},
		{
			name: "checksum mismatch",
			args: []string{"baseline", "-d", "app", "--all", "--yes"},
			setup: func(f *fakeMigrationAPI) {
				f.baselineStatus = http.StatusConflict
				f.baselineBody = map[string]any{"error": "001_create_notes.sql is recorded with a different checksum", "code": "checksum_mismatch"}
			},
			want:   "001_create_notes.sql is recorded with a different checksum",
			status: http.StatusConflict,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setTrackedMigrationTest(t)
			writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
			fake := newFakeMigrationAPI(t)
			if tc.setup != nil {
				tc.setup(fake)
			}
			server := fake.start()

			_, err := executeMigrationCommand(t, New(fake.deps(server)), tc.args...)
			require.ErrorContains(t, err, tc.want)
			if tc.status != 0 {
				assert.Equal(t, tc.status, api.Status(err))
			} else {
				assert.Empty(t, fake.recorded())
			}
		})
	}
}

func TestRunsListAndGet(t *testing.T) {
	setTrackedMigrationTest(t)
	fake := newFakeMigrationAPI(t)
	failed := migrationRun("failed", "apply", []string{"001_create_notes.sql:applied", "002_add_pinned.sql:failed", "003_add_tags.sql:not_run"}, map[string]any{
		"source":     "git_deploy",
		"commit_sha": "4f2a9c1e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a39",
		"error":      map[string]any{"code": "migration_failed", "message": "migration 002_add_pinned.sql failed at line 1 (SQLSTATE 42701)"},
	})
	files := failed["files"].([]any)
	files[1].(map[string]any)["error"] = map[string]any{"message": "column \"pinned\" already exists\x1b[31m", "sqlstate": "42701"}
	fake.runs = []map[string]any{failed}
	listed := migrationRun("failed", "apply", nil, map[string]any{"error": failed["error"]})
	fake.listedRuns = map[string]any{"data": []any{listed}}
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "runs", "list", "-d", "app")
	require.NoError(t, err, out)
	assert.Contains(t, out, migrationRunID)
	assert.Contains(t, out, "migration_failed")
	assert.Contains(t, out, "Showing 1 migration run(s) of database 'app'")

	out, err = executeMigrationCommand(t, New(fake.deps(server)), "runs", "get", migrationRunID, "-d", "app")
	require.NoError(t, err, out)
	for _, want := range []string{
		"ID: " + migrationRunID,
		"Status: failed",
		"Phase: apply",
		"Source: git_deploy 4f2a9c1e8b7d",
		"Error: migration_failed: migration 002_add_pinned.sql failed at line 1 (SQLSTATE 42701)",
		`column "pinned" already exists[31m (SQLSTATE 42701)`,
		"not run",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "\x1b", "the database's text cannot reach the terminal as an escape sequence")

	_, err = executeMigrationCommand(t, New(fake.deps(server)), "runs", "get", "not-a-uuid", "-d", "app")
	require.EqualError(t, err, `run ID "not-a-uuid" is not a UUID`)
}

// A run that ended while taking its backup may name one that was never
// created, so the CLI does not claim it was taken.
func TestApplyDoesNotClaimABackupItMayNotHaveTaken(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	backup := "before-migration-" + migrationRunID
	failed := migrationRun("failed", "backup", []string{"001_create_notes.sql:not_run"}, map[string]any{
		"backup_name": backup,
		"error":       map[string]any{"code": "backup_failed", "message": "the backup did not finish"},
	})
	fake := newFakeMigrationAPI(t)
	fake.runs = []map[string]any{failed}
	server := fake.start()

	out, err := executeMigrationCommand(t, New(fake.deps(server)), "apply", "-d", "app")
	require.Error(t, err)
	assert.Contains(t, out, "Backup "+backup+" may not exist: the run ended while taking it")
	assert.NotContains(t, out, "Backup taken first")

	out, err = executeMigrationCommand(t, New(fake.deps(server)), "runs", "get", migrationRunID, "-d", "app")
	require.NoError(t, err, out)
	assert.Contains(t, out, "Backup: "+backup+" (may not exist: the run ended while taking it)")
}

// The API takes only a lowercase .sql name, so the tracked commands refuse
// another casing before sending anything and say what to rename.
func TestTrackedCommandsRefuseAnUppercaseExtension(t *testing.T) {
	for _, args := range [][]string{
		{"apply", "-d", "app"},
		{"baseline", "-d", "app", "--all", "--yes"},
	} {
		t.Run(args[0], func(t *testing.T) {
			setTrackedMigrationTest(t)
			writeMigrationFile(t, "001_create_notes.SQL", "CREATE TABLE notes (id int);")
			fake := newFakeMigrationAPI(t)
			server := fake.start()

			_, err := executeMigrationCommand(t, New(fake.deps(server)), args...)
			require.EqualError(t, err, "volcano/migrations/001_create_notes.SQL: tracked migration runs take only file names "+
				"ending in lowercase .sql; rename it to 001_create_notes.sql")
			assert.Empty(t, fake.recorded())
			assert.Zero(t, fake.capReads)
		})
	}
}

// With --json, the confirmation goes to stderr so stdout holds only the
// JSON document.
func TestBaselineJSONKeepsThePromptOffStdout(t *testing.T) {
	setTrackedMigrationTest(t)
	writeMigrationFile(t, "001_create_notes.sql", "CREATE TABLE notes (id int);")
	fake := newFakeMigrationAPI(t)
	fake.baselineBody = map[string]any{"recorded": []string{"001_create_notes.sql"}}
	server := fake.start()

	cmd := New(fake.deps(server))
	var stdout, stderr bytes.Buffer
	cmd.SetIn(bytes.NewBufferString("yes\n"))
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"baseline", "-d", "app", "--all", "--json"})
	require.NoError(t, cmd.Execute(), stderr.String())

	var baseline struct {
		Recorded []string `json:"recorded"`
	}
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &baseline), stdout.String())
	assert.Equal(t, []string{"001_create_notes.sql"}, baseline.Recorded)
	assert.Contains(t, stderr.String(), "Record 1 file(s) as applied?")
}

// checksumOf is the checksum the history records for text that is already
// normalized.
func checksumOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func historyEntry(name, checksum, status string) map[string]any {
	return map[string]any{"name": name, "checksum": checksum, "status": status, "applied_at": "2026-10-29T09:00:00Z"}
}
