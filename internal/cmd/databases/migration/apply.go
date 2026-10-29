package migration

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

func newApply(deps cliruntime.Deps) *cobra.Command {
	var database string
	var noWait, jsonOutput bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply pending migrations in a tracked run",
		Long: fmt.Sprintf(`Send every migration file for a database to a tracked migration run, and wait
for it to end. Volcano applies only the files the database's migration history
does not record, each in its own transaction, and records them, so a file runs
once. It rehearses the pending files on a short-lived branch of the database
and takes a backup before it applies them, where the plan has branches and
backups.

A file may hold several statements. It cannot COMMIT, ROLLBACK, or use
statements that must run outside a transaction, such as CREATE INDEX
CONCURRENTLY.

A database migrated without tracking, with '%s' or an earlier
--with-migrations deploy, already reflects its files. Record them with '%s'
before the first run, or the run applies them again.

Tracked runs need the %q capability. Where the API does not offer it, the
command refuses before sending anything.

Usage:
  %s
  %s`,
			cliruntime.CommandPath(deps, "databases migration up"),
			cliruntime.CommandPath(deps, "databases migration baseline"),
			clidatabase.MigrationsCapability,
			cliruntime.CommandPath(deps, "databases migration apply -d mydb"),
			cliruntime.CommandPath(deps, "databases migration apply -d mydb --no-wait --json")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := requireDatabase(database)
			if err != nil {
				return err
			}
			return runApply(cmd.Context(), deps, name, !noWait, jsonOutput, cmd.OutOrStdout())
		},
	}
	databaseFlag(cmd, &database)
	cmd.Flags().BoolVar(&noWait, "no-wait", false, "Return once the run is queued")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit the run as machine-readable JSON")
	return cmd
}

func runApply(ctx context.Context, deps cliruntime.Deps, database string, wait, jsonOutput bool, out io.Writer) error {
	migrations, err := clidatabase.ReadMigrations(clidatabase.MigrationsDir)
	if err != nil {
		return err
	}
	files := migrations.For(database)
	if len(files) == 0 {
		if jsonOutput {
			return fmt.Errorf("no migration files for database %q in %s", database, migrations.Location(database))
		}
		fmt.Fprintf(out, "No migration files for database '%s' in %s; nothing to apply\n", database, migrations.Location(database))
		return nil
	}
	if err := clidatabase.CheckTrackedNames(files); err != nil {
		return err
	}
	c := commands{deps: deps, database: database}
	if err := c.explain(clidatabase.NewService(deps).RequireMigrationRuns(ctx)); err != nil {
		return err
	}
	progress := out
	if jsonOutput {
		progress = io.Discard
	}
	run, err := Apply(ctx, deps, ApplyOptions{
		Database: database,
		Files:    files,
		Location: migrations.Location(database),
		Wait:     wait,
		Out:      progress,
	})
	if jsonOutput && run != nil {
		if writeErr := writeJSON(out, run); writeErr != nil {
			return writeErr
		}
	}
	return err
}

// ApplyOptions configures Apply.
type ApplyOptions struct {
	Database string
	Files    []clidatabase.MigrationFile
	// Location names where the files came from, for the progress lines.
	Location string
	// Wait waits for the run to end; otherwise Apply returns once it is
	// queued.
	Wait bool
	Out  io.Writer
}

// Apply starts a tracked migration run of opts.Files on opts.Database and,
// with opts.Wait, waits for it to end. It returns the run as it last read it,
// and an error when the run failed or could not be followed to its end. The
// caller checks that the API offers tracked runs first.
func Apply(ctx context.Context, deps cliruntime.Deps, opts ApplyOptions) (*apiclient.DatabaseMigrationRun, error) {
	c := commands{deps: deps, database: opts.Database}
	on := theme.On(opts.Out)
	service := clidatabase.NewService(deps)
	fmt.Fprintf(opts.Out, "Starting a migration run of %d file(s) from %s on database '%s'\n", len(opts.Files), opts.Location, opts.Database)
	run, err := service.StartMigrationRun(ctx, opts.Database, opts.Files)
	if err != nil {
		return nil, c.explain(err)
	}
	fmt.Fprintf(opts.Out, "Run %s %s\n", run.Id, theme.Status(string(run.Status), on))
	if !opts.Wait {
		fmt.Fprintf(opts.Out, "%s%s\n", theme.Dim("Follow it with: ", on), theme.Command(c.runsGet(run.Id.String()), on))
		return run, nil
	}

	ended, err := service.WaitMigrationRun(ctx, opts.Database, run, func(latest *apiclient.DatabaseMigrationRun) {
		fmt.Fprintf(opts.Out, "  %s: %s\n", theme.Status(string(latest.Status), on), latest.Phase)
	})
	if err != nil {
		return ended, fmt.Errorf("stopped waiting for migration run %s on database %q, which carries on: %w\nFollow it with '%s'",
			run.Id, opts.Database, err, c.runsGet(run.Id.String()))
	}
	printRunEnd(opts.Out, ended)
	if ended.Status == apiclient.DatabaseMigrationRunStatusFailed {
		return ended, c.runFailure(ended)
	}
	return ended, nil
}

// printRunEnd prints how a run ended: the files it applied when it succeeded,
// and every file it did not settle as already applied when it failed.
func printRunEnd(w io.Writer, run *apiclient.DatabaseMigrationRun) {
	var files []apiclient.DatabaseMigrationRunFile
	if run.Files != nil {
		files = *run.Files
	}
	counts := map[apiclient.DatabaseMigrationRunFileOutcome]int{}
	for _, file := range files {
		counts[file.Outcome]++
	}
	var shown []apiclient.DatabaseMigrationRunFile
	for _, file := range files {
		if file.Outcome != apiclient.DatabaseMigrationRunFileOutcomeAlreadyApplied {
			shown = append(shown, file)
		}
	}
	if run.Status == apiclient.DatabaseMigrationRunStatusSucceeded {
		output.Success(w, "Migration run %s succeeded: %d applied, %d already applied", run.Id,
			counts[apiclient.DatabaseMigrationRunFileOutcomeApplied], counts[apiclient.DatabaseMigrationRunFileOutcomeAlreadyApplied])
	} else {
		output.Errorf(w, "Migration run %s failed in %s: %d applied, %d already applied", run.Id, run.Phase,
			counts[apiclient.DatabaseMigrationRunFileOutcomeApplied], counts[apiclient.DatabaseMigrationRunFileOutcomeAlreadyApplied])
	}
	if run.BackupName != nil && *run.BackupName != "" {
		if run.Phase == apiclient.DatabaseMigrationRunPhaseApply {
			fmt.Fprintf(w, "Backup taken first: %s\n", *run.BackupName)
		} else {
			fmt.Fprintf(w, "Backup %s may not exist: the run ended while taking it\n", *run.BackupName)
		}
	}
	output.MigrationRunFiles(w, shown)
	output.MigrationRunWarnings(w, run.Warnings)
}

// runFailure says why a run failed and what to do about it. The database's
// own message stays on the failed file, which printRunEnd showed: it can quote
// the file or its data, so it is kept out of the error. The run's message
// names the file, line, and SQLSTATE only.
func (c commands) runFailure(run *apiclient.DatabaseMigrationRun) error {
	code, message := "unknown", "the run reported no reason"
	if run.Error != nil {
		code, message = string(run.Error.Code), theme.StripControl(run.Error.Message)
	}
	err := fmt.Errorf("migration run %s failed in %s (%s): %s", run.Id, run.Phase, code, message)
	switch code {
	case string(apiclient.MigrationFailed):
		if run.Files != nil && !anyOutcome(*run.Files, apiclient.DatabaseMigrationRunFileOutcomeAlreadyApplied, apiclient.DatabaseMigrationRunFileOutcomeApplied) {
			return fmt.Errorf("%w\nIf database %q already reflects these files because they ran without tracking, record them with '%s' instead",
				err, c.database, c.baseline())
		}
	case string(apiclient.ChecksumMismatch):
		return fmt.Errorf("%w\nPut the file's recorded content back and make the change in a new file. '%s' shows which file changed",
			err, c.path("status"))
	case string(apiclient.HistoryMissing):
		return fmt.Errorf("%w\nRecord the files that were already applied with '%s'", err, c.path("baseline -f <file>"))
	}
	return err
}

func anyOutcome(files []apiclient.DatabaseMigrationRunFile, outcomes ...apiclient.DatabaseMigrationRunFileOutcome) bool {
	return slices.ContainsFunc(files, func(file apiclient.DatabaseMigrationRunFile) bool {
		return slices.Contains(outcomes, file.Outcome)
	})
}
