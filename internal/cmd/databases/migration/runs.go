package migration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/sensitive"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Server-side migration runs.
//
// Under a pg- delegated grant, `databases migration up` sends the files to
// Hosting's migration runs and follows the run, instead of reading the
// database's connection string and connecting to Postgres itself. A grant
// can't read a connection string (it is a credential that outlives the run),
// and the server picks the right database: in local mode the direct path used
// a fixed port, which could be another stack's database.
//
// There is no fallback to the direct path. Where migration runs are off (the
// `database_migrations` release gate), the command fails and says so.

// runPollInterval is how often a run is read while it has not ended. Tests set
// it to zero.
var runPollInterval = 2 * time.Second

// runWaitLimit bounds how long the command follows one run. The run carries on
// on the server after the command stops.
var runWaitLimit = 60 * time.Minute

// maxRunReadFailures is how many reads in a row may fail on the network before
// the command stops following a run.
const maxRunReadFailures = 5

// trackedMigrationName is the name rule the API applies to a top-level file:
// lowercase ".sql", no directory.
var trackedMigrationName = regexp.MustCompile(`^[^/\\]+\.sql$`)

// usesMigrationRuns reports whether up applies files through migration runs:
// when the credential is a delegated grant.
func usesMigrationRuns(sessions clisession.Factory) bool {
	cfg, err := sessions.Config()
	return err == nil && config.IsDelegatedGrant(cfg.Token())
}

// applyThroughRun applies files to database with a server-side migration run
// and follows the run to its end, printing what became of each file.
// baselineCommand is the command that records files as applied, named in the
// hint for a file that failed because its objects already exist.
func applyThroughRun(
	ctx context.Context, sessions clisession.Factory, database string, files []string, baselineCommand string, out io.Writer,
) error {
	project, err := sessions.CurrentProject()
	if err != nil {
		return err
	}
	request, err := migrationRunFiles(files)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "\nUsing database: %s\n", database)
	fmt.Fprintln(out, "Applying through a server-side migration run: files the database's history already records are skipped.")

	run, err := startRun(ctx, project, database, request, out)
	if err != nil {
		return err
	}
	run, err = followRun(ctx, project, database, run, out)
	if err != nil {
		return err
	}
	return reportRun(run, baselineCommand, out)
}

// migrationRunFiles reads files into a run request. Names go by base name,
// since up reads only the top level of volcano/migrations.
func migrationRunFiles(files []string) ([]apiclient.DatabaseMigrationFile, error) {
	request := make([]apiclient.DatabaseMigrationFile, 0, len(files))
	for _, file := range files {
		name := filepath.Base(file)
		if !trackedMigrationName.MatchString(name) {
			return nil, fmt.Errorf("migration %s: a migration run takes only names ending in lowercase .sql; rename it", name)
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", name, err)
		}
		request = append(request, apiclient.DatabaseMigrationFile{Name: name, Sql: string(content)})
	}
	return request, nil
}

// startRun starts the run. When the database already has a run going, it
// follows that run to its end and then starts this one: a run started by an
// earlier command whose grant expired while it waited ends first, and the new
// run skips whatever it applied.
func startRun(
	ctx context.Context, project *clisession.ProjectSession, database string, files []apiclient.DatabaseMigrationFile, out io.Writer,
) (*apiclient.DatabaseMigrationRun, error) {
	// One key per command: a retried request returns the run this command
	// started, and running the command again starts a new one.
	key := uuid.NewString()
	for attempt := 0; ; attempt++ {
		run, err := project.API.CreateDatabaseMigrationRun(ctx, project.ProjectID, database, key, files)
		if err == nil {
			fmt.Fprintf(out, "Started migration run %s.\n", run.Id)
			return run, nil
		}
		inProgress, ok := errors.AsType[*api.MigrationRunInProgressError](err)
		if !ok || attempt > 0 {
			return nil, migrationRunStartError(database, err)
		}
		fmt.Fprintf(out, "Migration run %s is already running on %s; waiting for it to end first.\n", inProgress.RunID, database)
		earlier, err := project.API.GetDatabaseMigrationRun(ctx, project.ProjectID, database, inProgress.RunID)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration run %s: %w", inProgress.RunID, err)
		}
		if _, err := followRun(ctx, project, database, earlier, out); err != nil {
			return nil, err
		}
	}
}

// migrationRunStartError says why a run did not start.
func migrationRunStartError(database string, err error) error {
	if api.Status(err) == http.StatusNotFound && api.Code(err) == api.FeatureUnavailableCode {
		return fmt.Errorf("migration runs are not available on this Volcano API, and a delegated grant "+
			"can't connect to the database directly, so nothing was applied to %q: %w", database, err)
	}
	return fmt.Errorf("failed to start a migration run on %q: %w", database, err)
}

// followRun reads run until it ends, printing each phase it enters.
func followRun(
	ctx context.Context, project *clisession.ProjectSession, database string, run *apiclient.DatabaseMigrationRun, out io.Writer,
) (*apiclient.DatabaseMigrationRun, error) {
	deadline := time.Now().Add(runWaitLimit)
	phase := apiclient.DatabaseMigrationRunPhase("")
	failures := 0
	for {
		if run.Phase != phase {
			phase = run.Phase
			fmt.Fprintf(out, "  %s: %s\n", run.Id, phase)
		}
		if runEnded(run) {
			return run, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("stopped waiting for migration run %s after %s; it carries on on the server. "+
				"Run the command again to wait for it", run.Id, runWaitLimit)
		}
		if err := sleep(ctx, runPollInterval); err != nil {
			return nil, err
		}
		next, err := project.API.GetDatabaseMigrationRun(ctx, project.ProjectID, database, run.Id)
		switch {
		case err == nil:
			run, failures = next, 0
		case api.Status(err) == 0 && failures+1 < maxRunReadFailures:
			// No answer from the API: a network failure, worth another read.
			failures++
		default:
			return nil, fmt.Errorf("stopped following migration run %s, which carries on on the server. "+
				"Run the command again to wait for it: %w", run.Id, err)
		}
	}
}

func runEnded(run *apiclient.DatabaseMigrationRun) bool {
	return run.Status == apiclient.DatabaseMigrationRunStatusSucceeded || run.Status == apiclient.DatabaseMigrationRunStatusFailed
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// reportRun prints each file's outcome and the run's warnings, and returns an
// error for a failed run. That error carries each failed file's own database
// error, from the rehearsal or the database, which names the object, so the
// caller can decide what to baseline. It is marked sensitive: it can quote SQL
// and data, so usage telemetry never sends it.
func reportRun(run *apiclient.DatabaseMigrationRun, baselineCommand string, out io.Writer) error {
	fmt.Fprintln(out)
	if run.Files != nil {
		for _, file := range *run.Files {
			fmt.Fprintf(out, "  %s: %s\n", file.Name, file.Outcome)
			if file.Error != nil {
				fmt.Fprintf(out, "    %s\n", fileErrorText(file.Error))
			}
		}
	}
	for _, warning := range run.Warnings {
		fmt.Fprintf(out, "Warning: %s\n", warning.Message)
	}
	if run.BackupName != nil && *run.BackupName != "" {
		fmt.Fprintf(out, "Backup: %s\n", *run.BackupName)
	}
	if run.Status == apiclient.DatabaseMigrationRunStatusSucceeded {
		fmt.Fprintln(out, "Migrations deployed!")
		return nil
	}
	summary := fmt.Sprintf("migration run %s failed in %s", run.Id, run.Phase)
	if run.Error != nil {
		summary += fmt.Sprintf(": %s: %s", run.Error.Code, run.Error.Message)
	}
	details := failedFileDetails(run, baselineCommand)
	if details == "" {
		return errors.New(summary)
	}
	return fmt.Errorf("%s\n%w", summary, sensitive.Wrap(errors.New(details)))
}

// duplicateObjectStates are the SQLSTATEs Postgres answers when an object a
// file creates already exists.
var duplicateObjectStates = map[string]bool{
	"42P04": true, // duplicate_database
	"42P06": true, // duplicate_schema
	"42P07": true, // duplicate_table
	"42701": true, // duplicate_column
	"42710": true, // duplicate_object
	"42712": true, // duplicate_alias
	"42723": true, // duplicate_function
}

// failedFileDetails is each failed file's database error, and for a file that
// failed because an object already exists, how to record it as applied.
func failedFileDetails(run *apiclient.DatabaseMigrationRun, baselineCommand string) string {
	if run.Files == nil {
		return ""
	}
	var lines []string
	for _, file := range *run.Files {
		if file.Error == nil {
			continue
		}
		lines = append(lines, "  "+file.Name+": "+fileErrorText(file.Error))
		if file.Error.Sqlstate != nil && duplicateObjectStates[*file.Error.Sqlstate] {
			lines = append(lines, fmt.Sprintf("  The database already holds an object %s creates. If it holds everything "+
				"the file creates, record it as applied with `%s -f %s --yes`, then run this again.",
				file.Name, baselineCommand, file.Name))
		}
	}
	return strings.Join(lines, "\n")
}

func fileErrorText(fileErr *apiclient.DatabaseMigrationFileError) string {
	var details []string
	if fileErr.Line != nil {
		details = append(details, fmt.Sprintf("line %d", *fileErr.Line))
	}
	if fileErr.Sqlstate != nil && *fileErr.Sqlstate != "" {
		details = append(details, "SQLSTATE "+*fileErr.Sqlstate)
	}
	if len(details) == 0 {
		return fileErr.Message
	}
	return fileErr.Message + " (" + strings.Join(details, ", ") + ")"
}
