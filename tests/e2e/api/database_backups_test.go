package api

import (
	"testing"
	"time"
)

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

	env.runCloudCLI(t, "databases", "backup-schedule", "set", database, "--frequency", "daily", "--hour", "3").
		requireSuccess(t, "Backup schedule of database '"+database+"' updated", "daily", "03:00")
	env.runCloudCLI(t, "databases", "backup-schedule", "get", database).requireSuccess(t, "daily", "03:00")
	env.runCloudCLI(t, "databases", "backup-schedule", "set", database, "--clear").
		requireSuccess(t, "Scheduled backups of database '"+database+"' stopped")
	env.runCloudCLI(t, "databases", "backup-schedule", "get", database).
		requireSuccess(t, "No scheduled backups of database '"+database+"'")

	// A point in time before the database existed is outside the restore window.
	env.runCloudCLI(t, "databases", "restore", database, "--to", "2020-01-15T09:30:00Z", "--yes").
		requireFailure(t, database)

	env.runCloudCLI(t, "databases", "restore", database, "--backup", backup, "--yes").
		requireSuccess(t, "Restore of database '"+database+"' started from backup '"+backup+"'", "pending")

	// The restore only claims the database: it returns with the database out of
	// service in 'restoring' and replaces the data in the background. The
	// connection string is unchanged throughout.
	restored := env.waitForCloudCLIContains(t, 30*time.Minute, "Status: active",
		"databases", "get", database, "--show-connection-string")
	restored.requireSuccess(t, "postgresql://")

	env.runCloudCLI(t, "databases", "backups", "delete", database, backup, "--yes").
		requireSuccess(t, "Backup '"+backup+"' of database '"+database+"' deleted")
	env.runCloudCLI(t, "databases", "backups", "list", database).requireNotContains(t, backup)
}
