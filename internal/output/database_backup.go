package output

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// DatabaseBackupCreated renders a backup that was just taken. A backup is
// restorable as soon as it is returned, so there is no status to wait on.
func DatabaseBackupCreated(w io.Writer, backup *apiclient.DatabaseBackup, databaseName string) {
	on := theme.On(w)
	Success(w, "Backup '%s' of database '%s' created", backup.Name, databaseName)
	kv(w, on, "Captures", "%s", FormatTimestamp(backup.CreatedAt))
	kv(w, on, "Expires", "%s", formatBackupExpiry(backup.ExpiresAt))
}

// DatabaseBackups renders a database's backups and the window a point-in-time
// restore may target.
func DatabaseBackups(w io.Writer, list *apiclient.DatabaseBackupList, databaseName string) {
	if list == nil {
		list = &apiclient.DatabaseBackupList{}
	}

	on := theme.On(w)
	if len(list.Data) == 0 {
		fmt.Fprintf(w, "No backups of database '%s'\n", databaseName)
		printRestoreWindow(w, on, list.RestoreWindow)
		return
	}

	tableHead(w, on, false, 96, "%-24s  %-10s  %-12s  %-15s  %-12s", "Name", "Source", "Size", "Captures", "Expires in")
	for _, backup := range list.Data {
		fmt.Fprintf(w, "%-24s  %-10s  %-12s  %-15s  %-12s\n",
			Truncate(backup.Name, 24),
			backupSource(backup),
			formatBackupSize(backup.SizeBytes),
			FormatTimeAgo(backup.CreatedAt),
			formatBackupRemaining(backup.ExpiresAt),
		)
	}
	summary(w, on, "Showing %d backup(s) of database '%s'", len(list.Data), databaseName)
	printRestoreWindow(w, on, list.RestoreWindow)
}

// DatabaseBackup renders one backup.
func DatabaseBackup(w io.Writer, backup *apiclient.DatabaseBackup) {
	on := theme.On(w)
	kv(w, on, "Name", "%s", backup.Name)
	kv(w, on, "Source", "%s", backupSource(*backup))
	kv(w, on, "Size", "%s", formatBackupSize(backup.SizeBytes))
	kv(w, on, "Captures", "%s", FormatTimestamp(backup.CreatedAt))
	kv(w, on, "Expires", "%s", formatBackupExpiry(backup.ExpiresAt))
}

// DatabaseRestoreStarted renders an accepted restore. The follow-up command
// watches the restore rather than the database, because that is what carries
// the reason when one does not finish.
func DatabaseRestoreStarted(w io.Writer, restore *apiclient.DatabaseRestore, databaseName string, commandPrefix ...string) {
	on := theme.On(w)
	Success(w, "Restore of database '%s' started %s", databaseName, restoreTargetPhrase(restore))
	kv(w, on, "ID", "%s", restore.Id.String())
	kv(w, on, "Status", "%s", theme.Status(restoreStatus(*restore), on))
	fmt.Fprintf(w, "\n%s%s\n",
		theme.Dim("The database does not serve connections until the restore finishes. Watch it with: ", on),
		theme.Command(fmt.Sprintf("%s databases restores get %s %s",
			commandPathPrefix(commandPrefix), databaseName, restore.Id.String()), on),
	)
}

// DatabaseRestores renders a database's restore history.
func DatabaseRestores(w io.Writer, list *apiclient.DatabaseRestoreList, databaseName string) {
	if list == nil {
		list = &apiclient.DatabaseRestoreList{}
	}

	on := theme.On(w)
	if len(list.Data) == 0 {
		fmt.Fprintf(w, "Database '%s' has never been restored\n", databaseName)
		return
	}

	tableHead(w, on, false, 96, "%-36s  %-10s  %-24s  %-15s", "ID", "Status", "Restored", "Started")
	for _, restore := range list.Data {
		fmt.Fprintf(w, "%-36s  %-10s  %-24s  %-15s\n",
			restore.Id.String(),
			theme.Status(restoreStatus(restore), on),
			Truncate(restoreTarget(&restore), 24),
			FormatTimeAgo(restore.CreatedAt),
		)
	}
	summary(w, on, "Showing %d restore(s) of database '%s'", len(list.Data), databaseName)
}

// DatabaseRestore renders one restore, including why it failed when it did.
func DatabaseRestore(w io.Writer, restore *apiclient.DatabaseRestore, databaseName string) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", restore.Id.String())
	kv(w, on, "Database", "%s", databaseName)
	kv(w, on, "Status", "%s", theme.Status(restoreStatus(*restore), on))
	kv(w, on, "Restored", "%s", restoreTarget(restore))
	kv(w, on, "Started", "%s", FormatTimestamp(restore.CreatedAt))
	if restore.CompletedAt != nil {
		kv(w, on, "Finished", "%s", FormatTimestamp(*restore.CompletedAt))
	}
	if restore.Error != nil && *restore.Error != "" {
		kv(w, on, "Error", "%s", *restore.Error)
	}
	printRestoreOutcome(w, on, restore)
}

// printRestoreOutcome says what the status means for the database, which is the
// question someone reading a restore actually has.
func printRestoreOutcome(w io.Writer, on bool, restore *apiclient.DatabaseRestore) {
	var note string
	switch restore.Status {
	case apiclient.DatabaseRestoreStatusPending, apiclient.DatabaseRestoreStatusRunning:
		note = "The database serves no connections until this finishes."
	case apiclient.DatabaseRestoreStatusFailed:
		note = "This attempt failed and another is coming. The database stays out of service until one lands."
	case apiclient.DatabaseRestoreStatusExhausted:
		note = "Every attempt failed and the database was left failed. Its data is whatever the last attempt left behind."
	default:
		return
	}
	fmt.Fprintf(w, "\n%s\n", theme.Dim(note, on))
}

// DatabaseBackupSchedule renders a database's automated backup schedule.
func DatabaseBackupSchedule(w io.Writer, schedule *apiclient.DatabaseBackupSchedule, databaseName string) {
	if schedule == nil {
		schedule = &apiclient.DatabaseBackupSchedule{}
	}

	on := theme.On(w)
	if len(schedule.Entries) == 0 {
		fmt.Fprintf(w, "No scheduled backups of database '%s'\n", databaseName)
		return
	}

	tableHead(w, on, false, 64, "%-10s  %-16s  %-12s", "Frequency", "When (UTC)", "Retention")
	for _, entry := range schedule.Entries {
		fmt.Fprintf(w, "%-10s  %-16s  %-12s\n",
			string(entry.Frequency),
			formatScheduleWhen(entry),
			formatScheduleRetention(entry.RetentionSeconds),
		)
	}
	summary(w, on, "Showing %d scheduled backup(s) of database '%s'", len(schedule.Entries), databaseName)
}

// printRestoreWindow reports the span a point-in-time restore may target. The
// API omits it on plans without point-in-time restore, so an absent window is
// not an error and prints nothing.
func printRestoreWindow(w io.Writer, on bool, window *apiclient.DatabaseRestoreWindow) {
	if window == nil || window.EarliestRestoreAt == nil || window.LatestRestoreAt == nil {
		return
	}
	summary(w, on, "Point-in-time restore window: %s to %s",
		FormatTimestamp(*window.EarliestRestoreAt), FormatTimestamp(*window.LatestRestoreAt))
}

func backupSource(backup apiclient.DatabaseBackup) string {
	source := strings.TrimSpace(string(backup.Source))
	if source == "" {
		return "-"
	}
	return source
}

func restoreStatus(restore apiclient.DatabaseRestore) string {
	status := strings.TrimSpace(string(restore.Status))
	if status == "" {
		return "-"
	}
	return status
}

// restoreTargetPhrase names what a restore is rewinding to, for the line that
// confirms it started.
func restoreTargetPhrase(restore *apiclient.DatabaseRestore) string {
	switch {
	case restore.BackupName != nil && *restore.BackupName != "":
		return fmt.Sprintf("from backup '%s'", *restore.BackupName)
	case restore.RestoreTo != nil:
		return "to " + FormatTimestamp(*restore.RestoreTo)
	default:
		return ""
	}
}

// restoreTarget names what a restore rewound to, for a column or a field. The
// backup name outlives the backup, so a restore stays readable after it is
// deleted.
func restoreTarget(restore *apiclient.DatabaseRestore) string {
	switch {
	case restore.BackupName != nil && *restore.BackupName != "":
		return *restore.BackupName
	case restore.RestoreTo != nil:
		return FormatTimestamp(*restore.RestoreTo)
	default:
		return "-"
	}
}

// formatBackupSize renders a backup's storage. The provider costs a backup a few
// minutes after taking it, so a fresh backup reports no size.
func formatBackupSize(sizeBytes *int64) string {
	if sizeBytes == nil {
		return "-"
	}
	return formatByteSize(*sizeBytes)
}

// formatBackupRemaining renders what is left of a backup's retention for the
// list view's column. A backup with no expiry is kept until deleted.
func formatBackupRemaining(expiresAt *time.Time) string {
	if expiresAt == nil {
		return "never"
	}
	remaining := time.Until(*expiresAt)
	if remaining <= 0 {
		return "expired"
	}
	return formatCompactDuration(remaining)
}

func formatBackupExpiry(expiresAt *time.Time) string {
	if expiresAt == nil {
		return "never (kept until deleted)"
	}
	return fmt.Sprintf("%s (%s)", FormatTimestamp(*expiresAt), formatBackupRemaining(expiresAt))
}

// Indexed by the API's weekday: 1 is Monday and 7 is Sunday, so index 0 is
// never a day.
var scheduleWeekdays = [8]string{"", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

// formatScheduleWhen renders when a recurrence fires. day carries a weekday for
// a weekly schedule and a day of the month for a monthly one, and is unused for
// a daily schedule.
func formatScheduleWhen(entry apiclient.DatabaseBackupScheduleEntry) string {
	at := fmt.Sprintf("%02d:00", entry.Hour)
	switch entry.Frequency {
	case "weekly":
		return fmt.Sprintf("%s %s", formatScheduleWeekday(entry.Day), at)
	case "monthly":
		return fmt.Sprintf("day %s %s", formatScheduleDayOfMonth(entry.Day), at)
	default:
		return at
	}
}

func formatScheduleWeekday(day *int) string {
	if day == nil || *day < 1 || *day >= len(scheduleWeekdays) {
		return "-"
	}
	return scheduleWeekdays[*day]
}

func formatScheduleDayOfMonth(day *int) string {
	if day == nil {
		return "-"
	}
	return strconv.Itoa(*day)
}

func formatScheduleRetention(retentionSeconds *int64) string {
	if retentionSeconds == nil {
		return "plan default"
	}
	return formatCompactDuration(time.Duration(*retentionSeconds) * time.Second)
}
