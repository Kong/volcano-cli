package api

import (
	"regexp"
	"testing"
	"time"
)

// TestAPIE2ECloudDatabaseBackups drives every backup, restore and schedule
// command against a real platform. The unit tests already cover flag parsing,
// rendering and error mapping against a fake server; what only a deployed
// environment can answer is whether the values the CLI sends are the ones the
// platform accepts — a weekday numbered from the wrong end, a retention in the
// wrong unit, or a timestamp in the wrong format all pass a fake and fail here.
func TestAPIE2ECloudDatabaseBackups(t *testing.T) {
	env := setupAPIE2E(t, "cloud-database-backups")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	database := "cli_e2e_" + apiE2ESuffix(t)
	env.runCloudCLI(t, "databases", "create", database, "--region", apiE2EDefaultRegion, "--pg-version", apiE2EDefaultPGVersion).requireSuccess(t, database)
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "databases", "delete", database, "--yes")
	})
	env.waitForDatabaseActive(t, database, 30*time.Minute)

	// Backups are Pro, and the refusal reaches the reads too, so a Free project
	// gets the plan's reason rather than an empty list or a token error. Asserted
	// before the upgrade because a new user starts on Free, which is also why the
	// rest of this test would otherwise be reading 403s.
	env.runCloudCLI(t, "databases", "backups", "list", database).
		requireFailure(t, "backups are not available on this plan")
	env.runCloudCLI(t, "databases", "backup-schedule", "get", database).
		requireFailure(t, "backups are not available on this plan")
	env.runCloudCLI(t, "databases", "restores", "list", database).
		requireFailure(t, "backups are not available on this plan")

	// Paying unlocks the database the project already has, with nothing to
	// re-create. Polled rather than asserted once: a plan change reaches the API
	// through a cache invalidation, so the flip is quick but not synchronous with
	// the write. The window arriving with it is the point-in-time entitlement,
	// which the list is also where a caller reads.
	env.setUserPlan(t, "PRO")
	unlocked := env.waitForCloudCLIContains(t, 5*time.Minute, "Point-in-time restore window:",
		"databases", "backups", "list", database)
	unlocked.requireSuccess(t, "No backups of database '"+database+"'")

	// A backup name is lowercase alphanumeric with underscores and hyphens.
	backup := "cli_e2e_backup"
	env.runCloudCLI(t, "databases", "backups", "create", database, backup).
		requireSuccess(t, "Backup '"+backup+"' of database '"+database+"' created")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "databases", "backups", "delete", database, backup, "--yes")
	})

	env.runCloudCLI(t, "databases", "backups", "get", database, backup).
		requireSuccess(t, "Name: "+backup, "Source: manual")
	env.runCloudCLI(t, "databases", "backups", "list", database).requireSuccess(t, backup, "manual")

	// A backup name is unique within its database.
	env.runCloudCLI(t, "databases", "backups", "create", database, backup).requireFailure(t, backup)

	// The reserved prefix is the platform's own, and the refusal is the API's
	// rather than a client-side name check.
	env.runCloudCLI(t, "databases", "backups", "create", database, "volcano-mine").
		requireFailure(t, "are reserved")

	// The singular aliases are part of the advertised surface, so they have to
	// reach the same routes.
	env.runCloudCLI(t, "databases", "backup", "list", database).requireSuccess(t, backup)
	env.runCloudCLI(t, "databases", "restore-history", "list", database).
		requireSuccess(t, "has never been restored")

	// A name that exists nowhere reads as missing rather than as a failure of the
	// command, on each of the three things that can be named.
	env.runCloudCLI(t, "databases", "backups", "get", database, "cli_e2e_absent").
		requireFailure(t, "backup not found")
	env.runCloudCLI(t, "databases", "backups", "delete", database, "cli_e2e_absent", "--yes").
		requireFailure(t, "backup not found")
	env.runCloudCLI(t, "databases", "backups", "list", "cli_e2e_absent_db").
		requireFailure(t, "database not found")
	env.runCloudCLI(t, "databases", "restores", "get", database, "01234567-89ab-cdef-0123-456789abcdef").
		requireFailure(t, "restore not found")

	env.assertScheduleRoundTrips(t, database)
	env.assertPointInTimeRestore(t, database)
	env.assertSnapshotRestore(t, database, backup)

	env.runCloudCLI(t, "databases", "backups", "delete", database, backup, "--yes").
		requireSuccess(t, "Backup '"+backup+"' of database '"+database+"' deleted")
	env.runCloudCLI(t, "databases", "backups", "list", database).requireNotContains(t, backup)
}

// assertScheduleRoundTrips sets every frequency the platform takes and reads
// each one back.
//
// The weekday and the day of the month are the values worth spending a real API
// on: the platform numbers the week 1-7 from Monday, and the CLI once numbered it
// 0-6, which named the day before the one asked for and left Sunday unreachable.
// A fake server accepts either. Retention is the same kind of contract in a
// different unit — the flag is a Go duration and the API takes seconds.
func (e *apiE2E) assertScheduleRoundTrips(t *testing.T, database string) {
	t.Helper()

	e.runCloudCLI(t, "databases", "backup-schedule", "set", database, "--frequency", "daily", "--hour", "3").
		requireSuccess(t, "Backup schedule of database '"+database+"' updated", "daily", "03:00")
	e.runCloudCLI(t, "databases", "backup-schedule", "get", database).requireSuccess(t, "daily", "03:00")

	// Sunday is day 7, the end the CLI used to get wrong, and a replace rather
	// than an addition: the schedule that comes back is only the new one.
	e.runCloudCLI(t, "databases", "backup-schedule", "set", database,
		"--frequency", "weekly", "--hour", "4", "--day", "7").
		requireSuccess(t, "weekly", "Sunday 04:00")
	weekly := e.runCloudCLI(t, "databases", "backup-schedule", "get", database)
	weekly.requireSuccess(t, "weekly", "Sunday 04:00", "Showing 1 scheduled backup(s)")
	weekly.requireNotContains(t, "daily")

	// Monday is the other end, and the one an off-by-one names as Sunday.
	e.runCloudCLI(t, "databases", "backup-schedule", "set", database,
		"--frequency", "weekly", "--hour", "5", "--day", "1").
		requireSuccess(t, "Monday 05:00")

	e.runCloudCLI(t, "databases", "backup-schedule", "set", database,
		"--frequency", "monthly", "--hour", "6", "--day", "28").
		requireSuccess(t, "monthly", "day 28 06:00")
	e.runCloudCLI(t, "databases", "backup-schedule", "get", database).
		requireSuccess(t, "monthly", "day 28 06:00")

	// A retention inside the plan's survives the round trip in the unit the API
	// keeps it in: 168h of flag becomes 7d of rendered schedule.
	e.runCloudCLI(t, "databases", "backup-schedule", "set", database,
		"--frequency", "daily", "--hour", "7", "--retention", "168h").
		requireSuccess(t, "7d")
	e.runCloudCLI(t, "databases", "backup-schedule", "get", database).requireSuccess(t, "7d")

	// Past the plan's retention the platform clamps rather than refuses, so the
	// schedule that comes back is the one that will actually run.
	e.runCloudCLI(t, "databases", "backup-schedule", "set", database,
		"--frequency", "daily", "--hour", "8", "--retention", "2000h").
		requireSuccess(t, "30d")

	e.runCloudCLI(t, "databases", "backup-schedule", "set", database, "--clear").
		requireSuccess(t, "Scheduled backups of database '"+database+"' stopped")
	e.runCloudCLI(t, "databases", "backup-schedule", "get", database).
		requireSuccess(t, "No scheduled backups of database '"+database+"'")

	// Left in place for the restores that follow. The provider keeps a schedule on
	// the branch it belongs to and a restore moves the data onto a new branch, so
	// a schedule still here afterwards is one the restore carried across —
	// automated backups have to survive it, because nobody would think to set
	// them again.
	e.runCloudCLI(t, "databases", "backup-schedule", "set", database, "--frequency", "daily", "--hour", "2").
		requireSuccess(t, "daily", "02:00")
}

// assertScheduleSurvivedRestore checks the schedule the restores were supposed
// to carry onto the branch the database now lives on.
func (e *apiE2E) assertScheduleSurvivedRestore(t *testing.T, database string) {
	t.Helper()
	e.runCloudCLI(t, "databases", "backup-schedule", "get", database).requireSuccess(t, "daily", "02:00")
}

// assertPointInTimeRestore restores to a timestamp rather than to a backup.
//
// Runs before the snapshot restore because a restore moves the database onto a
// new branch, and the provider's history window starts over with it: the reachable
// past here is the database's own, which is what the window the CLI prints is
// describing.
func (e *apiE2E) assertPointInTimeRestore(t *testing.T, database string) {
	t.Helper()

	// A time before the database existed is refused for the reason the caller can
	// act on, and names the span to choose within. Asserted rather than taken as
	// any failure: the same status covers a timestamp the API could not parse,
	// which is the mistake this is really guarding.
	e.runCloudCLI(t, "databases", "restore", database, "--to", "2020-01-15T09:30:00Z", "--yes").
		requireFailure(t, "outside the available window", "choose a time between")

	// The provider's history is measured in seconds, so the mark has to be far
	// enough behind to be unambiguously reachable.
	time.Sleep(60 * time.Second)
	mark := time.Now().UTC()
	time.Sleep(60 * time.Second)

	started := e.runCloudCLI(t, "databases", "restore", database, "--to", mark.Format(time.RFC3339), "--yes")
	started.requireSuccess(t, "Restore of database '"+database+"' started", "pending")
	restoreID := apiE2ERestoreID(t, started.output)

	// A point-in-time restore names the moment it rewound to where a snapshot
	// restore names the backup, which is the only way a caller tells them apart.
	e.runCloudCLI(t, "databases", "restores", "get", database, restoreID).
		requireSuccess(t, "ID: "+restoreID, "Restored: "+mark.Local().Format(time.RFC3339))

	e.waitForCloudCLIContains(t, 30*time.Minute, "Status: active", "databases", "get", database)
	e.runCloudCLI(t, "databases", "restores", "get", database, restoreID).requireSuccess(t, "Status: completed")
	e.assertScheduleSurvivedRestore(t, database)
}

// assertSnapshotRestore restores the named backup and leaves the database
// serving its data again.
func (e *apiE2E) assertSnapshotRestore(t *testing.T, database, backup string) {
	t.Helper()

	started := e.runCloudCLI(t, "databases", "restore", database, "--backup", backup, "--yes")
	started.requireSuccess(t, "Restore of database '"+database+"' started from backup '"+backup+"'", "pending")
	restoreID := apiE2ERestoreID(t, started.output)

	// The restore is readable while it runs, and is the only thing that reports
	// how it went: the database says 'restoring' and nothing more.
	e.runCloudCLI(t, "databases", "restores", "list", database).requireSuccess(t, restoreID, backup)
	e.runCloudCLI(t, "databases", "restores", "get", database, restoreID).
		requireSuccess(t, "ID: "+restoreID, "Restored: "+backup)

	// A restore holds the database while it runs, and the CLI has to pass that
	// refusal through as itself rather than as some other failure. Conditional on
	// timing rather than skipped: the restore may already have finished, in which
	// case the create legitimately succeeds — but if it fails, it has to fail for
	// this reason.
	if conflict := e.runCloudCLI(t, "databases", "backups", "create", database, "cli_e2e_during"); conflict.code != 0 {
		conflict.requireFailure(t, "a restore is already in progress")
	}

	// The restore only claims the database: it returns with the database out of
	// service in 'restoring' and replaces the data in the background. The
	// connection string is unchanged throughout.
	restored := e.waitForCloudCLIContains(t, 30*time.Minute, "Status: active",
		"databases", "get", database, "--show-connection-string")
	restored.requireSuccess(t, "postgresql://")

	e.runCloudCLI(t, "databases", "restores", "get", database, restoreID).
		requireSuccess(t, "Status: completed")

	e.assertScheduleSurvivedRestore(t, database)

	// Two restores have run, and the history is newest first.
	history := e.runCloudCLI(t, "databases", "restores", "list", database)
	history.requireSuccess(t, restoreID, "Showing 2 restore(s)")
}

var apiE2ERestoreIDLine = regexp.MustCompile(`(?m)^ID:\s+(\S+)`)

// apiE2ERestoreID pulls the restore id out of what the restore command printed,
// which is the only place the caller gets one.
func apiE2ERestoreID(t *testing.T, output string) string {
	t.Helper()
	match := apiE2ERestoreIDLine.FindStringSubmatch(output)
	if match == nil {
		t.Fatalf("restore output named no id:\n%s", output)
	}
	return match[1]
}
