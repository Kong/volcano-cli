package migration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// usesMigrationRuns reports whether up applies files through migration runs:
// when the credential is a delegated grant.
func usesMigrationRuns(sessions clisession.Factory) bool {
	cfg, err := sessions.Config()
	return err == nil && config.IsDelegatedGrant(cfg.Token())
}

// applyThroughRun applies files to database with a server-side migration run
// and follows the run to its end, printing what became of each file.
func applyThroughRun(ctx context.Context, sessions clisession.Factory, database string, files []string, out io.Writer) error {
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
	return reportRun(run, out)
}

// migrationRunFiles reads files into a run request. Names go by base name,
// since up reads only the top level of volcano/migrations.
func migrationRunFiles(files []string) ([]apiclient.DatabaseMigrationFile, error) {
	request := make([]apiclient.DatabaseMigrationFile, 0, len(files))
	for _, file := range files {
		name := filepath.Base(file)
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", name, err)
		}
		request = append(request, apiclient.DatabaseMigrationFile{Name: name, Sql: string(content)})
	}
	return request, nil
}

// startRun starts the run. A run already going on the database is the API's
// 409, which names it; the caller waits for it and runs the command again.
func startRun(
	ctx context.Context, project *clisession.ProjectSession, database string, files []apiclient.DatabaseMigrationFile, out io.Writer,
) (*apiclient.DatabaseMigrationRun, error) {
	// A retried request with this key returns the run it started.
	run, err := project.API.CreateDatabaseMigrationRun(ctx, project.ProjectID, database, uuid.NewString(), files)
	if err != nil {
		return nil, migrationRunStartError(database, err)
	}
	fmt.Fprintf(out, "Started migration run %s.\n", run.Id)
	return run, nil
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
func reportRun(run *apiclient.DatabaseMigrationRun, out io.Writer) error {
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
	details := failedFileDetails(run)
	if details == "" {
		return errors.New(summary)
	}
	return fmt.Errorf("%s\n%w", summary, sensitive.Wrap(errors.New(details)))
}

// failedFileDetails is each failed file's database error, which names the
// object, so the caller can decide what to baseline.
func failedFileDetails(run *apiclient.DatabaseMigrationRun) string {
	if run.Files == nil {
		return ""
	}
	var lines []string
	for _, file := range *run.Files {
		if file.Error == nil {
			continue
		}
		lines = append(lines, "  "+file.Name+": "+fileErrorText(file.Error))
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
