package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/theme"
)

// MigrationCommands are the commands the migration renderers suggest, already
// completed with the database.
type MigrationCommands struct {
	Apply    string
	Baseline string
	// RunsGet returns the command that shows a run.
	RunsGet func(runID string) string
}

// DatabaseMigrationStatus renders where each migration file stands against a
// database's migration history. location names where the files live.
func DatabaseMigrationStatus(w io.Writer, status clidatabase.MigrationStatus, location string, commands MigrationCommands) {
	on := theme.On(w)
	if !status.Compared {
		fmt.Fprintf(w, "No migration files for database '%s' in %s; showing its history only.\n\n", status.Database, location)
	}
	if len(status.Files) == 0 {
		fmt.Fprintf(w, "Database '%s' has no migration history\n", status.Database)
	} else {
		tableHead(w, on, false, 72, "%-44s  %-10s  %-15s", "Name", "State", "Recorded")
		for _, file := range status.Files {
			recorded := "-"
			if file.AppliedAt != nil {
				recorded = FormatTimeAgo(*file.AppliedAt)
			}
			fmt.Fprintf(w, "%-44s  %s  %-15s\n",
				Truncate(theme.StripControl(file.Name), 44), migrationStateCell(file.State, on), recorded)
		}
		var counts []string
		for _, state := range []clidatabase.MigrationState{
			clidatabase.MigrationApplied, clidatabase.MigrationBaselined, clidatabase.MigrationPending,
			clidatabase.MigrationChanged, clidatabase.MigrationMissing,
		} {
			if count := status.Count(state); count > 0 {
				counts = append(counts, fmt.Sprintf("%d %s", count, state))
			}
		}
		summary(w, on, "%s", strings.Join(counts, ", "))
	}

	if status.ActiveRunID != nil {
		fmt.Fprintf(w, "\nRun %s is queued or running. Follow it with: %s\n",
			status.ActiveRunID, theme.Command(commands.RunsGet(status.ActiveRunID.String()), on))
	}
	if status.Count(clidatabase.MigrationChanged) > 0 {
		fmt.Fprintf(w, "\n%s\n", theme.Dim("A run refuses an applied file whose content changed (checksum_mismatch). "+
			"Put its recorded content back and make the change in a new file.", on))
	}
	if status.Count(clidatabase.MigrationPending) > 0 {
		fmt.Fprintf(w, "\n%s%s\n", theme.Dim("Apply the pending files with: ", on), theme.Command(commands.Apply, on))
		if status.Count(clidatabase.MigrationApplied)+status.Count(clidatabase.MigrationBaselined) == 0 {
			fmt.Fprintf(w, "%s%s\n",
				theme.Dim("If they already ran without tracking, record them instead with: ", on),
				theme.Command(commands.Baseline, on))
		}
	}
}

func migrationStateCell(state clidatabase.MigrationState, on bool) string {
	text := fmt.Sprintf("%-10s", state)
	switch state {
	case clidatabase.MigrationApplied, clidatabase.MigrationBaselined:
		return theme.Success(text, on)
	case clidatabase.MigrationPending:
		return theme.Warn(text, on)
	case clidatabase.MigrationChanged:
		return theme.Fail(text, on)
	default:
		return theme.Dim(text, on)
	}
}

// DatabaseMigrationRun renders one migration run with each file's outcome.
// A failed file's message is the database's own and can quote the file or
// its data, so it is printed, never sent anywhere.
func DatabaseMigrationRun(w io.Writer, run *apiclient.DatabaseMigrationRun) {
	on := theme.On(w)
	kv(w, on, "ID", "%s", run.Id.String())
	kv(w, on, "Database", "%s", run.Database.Name)
	kv(w, on, "Status", "%s", theme.Status(string(run.Status), on))
	kv(w, on, "Phase", "%s", run.Phase)
	kv(w, on, "Source", "%s", migrationRunSource(run))
	if run.BackupName != nil && *run.BackupName != "" {
		backup := *run.BackupName
		if run.Phase == apiclient.DatabaseMigrationRunPhaseBackup {
			if run.Status == apiclient.DatabaseMigrationRunStatusFailed {
				backup += " (may not exist: the run ended while taking it)"
			} else {
				backup += " (being taken)"
			}
		}
		kv(w, on, "Backup", "%s", backup)
	}
	kv(w, on, "Created", "%s", FormatTimestamp(run.CreatedAt))
	if run.CompletedAt != nil {
		kv(w, on, "Finished", "%s", FormatTimestamp(*run.CompletedAt))
	}
	if run.Error != nil {
		kv(w, on, "Error", "%s: %s", run.Error.Code, theme.StripControl(run.Error.Message))
	}
	if run.Files != nil {
		MigrationRunFiles(w, *run.Files)
	}
	MigrationRunWarnings(w, run.Warnings)
}

// MigrationRunFiles renders what became of each file of a run.
func MigrationRunFiles(w io.Writer, files []apiclient.DatabaseMigrationRunFile) {
	on := theme.On(w)
	if len(files) == 0 {
		return
	}
	tableHead(w, on, true, 64, "%-44s  %-15s", "File", "Outcome")
	for _, file := range files {
		fmt.Fprintf(w, "%-44s  %s\n", Truncate(theme.StripControl(file.Name), 44), migrationOutcomeCell(file.Outcome, on))
		if file.Error != nil {
			fmt.Fprintf(w, "  %s\n", theme.Fail(migrationFileError(file.Error), on))
		}
	}
}

func migrationOutcomeCell(outcome apiclient.DatabaseMigrationRunFileOutcome, on bool) string {
	text := strings.ReplaceAll(string(outcome), "_", " ")
	switch outcome {
	case apiclient.DatabaseMigrationRunFileOutcomeApplied:
		return theme.Success(text, on)
	case apiclient.DatabaseMigrationRunFileOutcomeFailed, apiclient.DatabaseMigrationRunFileOutcomeUnknown:
		return theme.Fail(text, on)
	case apiclient.DatabaseMigrationRunFileOutcomePending:
		return theme.Warn(text, on)
	default:
		return theme.Dim(text, on)
	}
}

func migrationFileError(fileErr *apiclient.DatabaseMigrationFileError) string {
	var where []string
	if fileErr.Sqlstate != nil && *fileErr.Sqlstate != "" {
		where = append(where, "SQLSTATE "+*fileErr.Sqlstate)
	}
	if fileErr.Line != nil {
		where = append(where, fmt.Sprintf("line %d", *fileErr.Line))
	}
	message := theme.StripControl(fileErr.Message)
	if len(where) == 0 {
		return message
	}
	return fmt.Sprintf("%s (%s)", message, strings.Join(where, ", "))
}

// MigrationRunWarnings renders the conditions a run reported that did not
// stop it.
func MigrationRunWarnings(w io.Writer, warnings []apiclient.DatabaseMigrationWarning) {
	if len(warnings) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, warning := range warnings {
		Warning(w, "%s: %s", warning.Code, theme.StripControl(warning.Message))
	}
}

// DatabaseMigrationRuns renders a database's recent migration runs.
func DatabaseMigrationRuns(w io.Writer, list *apiclient.DatabaseMigrationRunList, databaseName string) {
	if list == nil {
		list = &apiclient.DatabaseMigrationRunList{}
	}
	on := theme.On(w)
	if len(list.Data) == 0 {
		fmt.Fprintf(w, "No migration runs on database '%s'\n", databaseName)
		return
	}
	tableHead(w, on, false, 100, "%-36s  %-10s  %-9s  %-11s  %-20s  %-15s", "ID", "Status", "Phase", "Source", "Error", "Created")
	for _, run := range list.Data {
		errorCode := "-"
		if run.Error != nil {
			errorCode = string(run.Error.Code)
		}
		fmt.Fprintf(w, "%-36s  %s  %-9s  %-11s  %-20s  %-15s\n",
			run.Id.String(),
			statusCell(string(run.Status), 10, on),
			run.Phase,
			migrationRunSource(&run),
			Truncate(errorCode, 20),
			FormatTimeAgo(run.CreatedAt),
		)
	}
	summary(w, on, "Showing %d migration run(s) of database '%s'", len(list.Data), databaseName)
}

func migrationRunSource(run *apiclient.DatabaseMigrationRun) string {
	source := string(run.Source)
	if run.CommitSha != nil && *run.CommitSha != "" {
		sha := *run.CommitSha
		source += " " + sha[:min(len(sha), 12)]
	}
	return source
}

// DatabaseMigrationBaseline renders the files a baseline recorded.
func DatabaseMigrationBaseline(w io.Writer, baseline *apiclient.DatabaseMigrationBaseline, databaseName string) {
	if len(baseline.Recorded) == 0 {
		Success(w, "Nothing to record: the migration history of database '%s' already holds every file", databaseName)
		return
	}
	Success(w, "Recorded %d file(s) as applied on database '%s', without running them", len(baseline.Recorded), databaseName)
	for _, name := range baseline.Recorded {
		fmt.Fprintf(w, "  - %s\n", theme.StripControl(name))
	}
}
