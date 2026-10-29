package migration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type sqlExecutor func(context.Context, string, string) error

type upOptions struct {
	deps     cliruntime.Deps
	database string
	file     string
	all      bool
	executor sqlExecutor
	out      io.Writer
}

// New returns the migration command.
func New(deps cliruntime.Deps) *cobra.Command {
	return newWithExecutor(deps, executeSQLMigration)
}

// NewLocal returns the local migrations command.
func NewLocal(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrations",
		Short: "Run local database migrations without tracking",
		Long: "Run SQL migrations from the volcano/migrations directory on a local database, without tracking.\n" +
			"To apply each file once, use '" + cliruntime.CommandPath(deps, "databases migration apply -d app") + "'.",
	}
	cmd.AddCommand(newDeploy(deps, executeSQLMigration))
	return cmd
}

func newWithExecutor(deps cliruntime.Deps, executor sqlExecutor) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migration",
		Short: "Manage database migrations",
		Long: `Apply SQL migrations from the volcano/migrations directory.

apply sends a database's files to a tracked migration run: Volcano records
each file it applies in the database's migration history, so a file runs once.
status compares the files with that history, baseline records files that
already ran without running them, and runs lists the runs. up runs files
directly, without tracking, so every file runs again on each call.

With one database, put the files in volcano/migrations. With several, put each
database's files in volcano/migrations/<database>/. Names starting with _ or .
are skipped.`,
	}
	cmd.AddCommand(newStatus(deps))
	cmd.AddCommand(newApply(deps))
	cmd.AddCommand(newBaseline(deps))
	cmd.AddCommand(newRuns(deps))
	cmd.AddCommand(newUp(deps, executor))
	return cmd
}

func newUp(deps cliruntime.Deps, executor sqlExecutor) *cobra.Command {
	return newUntracked(deps, executor, "up", fmt.Sprintf(`Run SQL migration files from volcano/migrations/ in name order, without
tracking.

Usage:
  %s
  %s
  %s
  %s

The --database flag is required. The CLI connects directly to your database via
pgproxy and executes the SQL files, one statement per file.

WARNING: Migrations are executed without tracking. Every file runs again on each
call, whether or not the database's migration history records it, so you are
responsible for not re-running non-idempotent migrations. To apply each file
once, use '%s'.`,
		cliruntime.CommandPath(deps, "databases migration up --all -d mydb"),
		cliruntime.CommandPath(deps, "databases migration up -a -d mydb"),
		cliruntime.CommandPath(deps, "databases migration up -d mydb -f 001_create_users"),
		cliruntime.CommandPath(deps, "databases migration up --database mydb -f 001_create_users.sql"),
		cliruntime.CommandPath(deps, "databases migration apply -d mydb")))
}

func newDeploy(deps cliruntime.Deps, executor sqlExecutor) *cobra.Command {
	return newUntracked(deps, executor, "deploy", fmt.Sprintf(`Run SQL migration files from volcano/migrations/ in name order, without
tracking.

Usage:
  %s
  %s
  %s
  %s

The --database flag is required. The CLI connects directly to your database and
executes the SQL files, one statement per file.

WARNING: Migrations are executed without tracking. Every file runs again on each
call, whether or not the database's migration history records it, so you are
responsible for not re-running non-idempotent migrations. To apply each file
once, use '%s'.`,
		cliruntime.CommandPath(deps, "migrations deploy --all -d mydb"),
		cliruntime.CommandPath(deps, "migrations deploy -a -d mydb"),
		cliruntime.CommandPath(deps, "migrations deploy -d mydb -f 001_create_users"),
		cliruntime.CommandPath(deps, "migrations deploy --database mydb -f 001_create_users.sql"),
		cliruntime.CommandPath(deps, "databases migration apply -d mydb")))
}

func newUntracked(deps cliruntime.Deps, executor sqlExecutor, use, long string) *cobra.Command {
	var database string
	var file string
	var all bool
	cmd := &cobra.Command{
		Use:   use,
		Short: "Run migration files without tracking",
		Long:  long,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUp(cmd.Context(), upOptions{
				deps:     deps,
				database: strings.TrimSpace(database),
				file:     strings.TrimSpace(file),
				all:      all,
				executor: executor,
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Apply a specific migration by name or path")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Apply all migrations")
	cmd.Flags().StringVarP(&database, "database", "d", "", "Database name to run migrations against (required)")
	if err := cmd.MarkFlagRequired("database"); err != nil {
		panic(err)
	}
	return cmd
}

func runUp(ctx context.Context, opts upOptions) error {
	if opts.executor == nil {
		opts.executor = executeSQLMigration
	}
	if opts.database == "" {
		return errors.New("specify the database with --database/-d")
	}
	if opts.all && opts.file != "" {
		return errors.New("cannot use --all and --file together")
	}
	if !opts.all && opts.file == "" {
		return errors.New("specify either --all to apply all migrations or --file/-f to apply a specific migration")
	}

	if opts.all {
		fmt.Fprintf(opts.out, "\nScanning %s...\n", clidatabase.MigrationsDir)
	}

	migrations, err := clidatabase.ReadMigrations(clidatabase.MigrationsDir)
	if err != nil {
		return fmt.Errorf("failed to scan migrations: %w", err)
	}
	allMigrations := migrations.For(opts.database)

	if len(allMigrations) == 0 {
		location := migrations.Location(opts.database)
		if opts.file != "" {
			return fmt.Errorf("no migration files found in %s/\nnote: migrations must be placed in the volcano/migrations/ directory, or in volcano/migrations/<database>/", location)
		}
		fmt.Fprintf(opts.out, "No migration files found in %s/\n", location)
		return nil
	}

	migrationFiles, err := selectMigrationFiles(allMigrations, opts.file, opts.all)
	if err != nil {
		return err
	}

	if opts.all {
		fmt.Fprintf(opts.out, "Found %d file(s) (sorted alphabetically):\n", len(migrationFiles))
		for _, file := range migrationFiles {
			fmt.Fprintf(opts.out, "  - %s\n", file.Name())
		}
	} else {
		fmt.Fprintf(opts.out, "\nExecuting migration: %s\n", migrationFiles[0].Name())
	}

	database, err := clidatabase.NewService(opts.deps).Get(ctx, opts.database)
	if err != nil {
		return err
	}
	if database.Status != apiclient.DatabaseStatusActive {
		return fmt.Errorf("database %q is not active (status: %s)", opts.database, database.Status)
	}
	connectionString := stringPtrValue(database.ConnectionString)
	if connectionString == "" {
		return fmt.Errorf("database %q does not have a connection string", opts.database)
	}

	fmt.Fprintf(opts.out, "\nUsing database: %s\n", opts.database)
	fmt.Fprintln(opts.out, "Warning: Migrations will be executed without tracking.")
	fmt.Fprintln(opts.out, "   Make sure these haven't been run before!")
	fmt.Fprintln(opts.out)

	for _, file := range migrationFiles {
		fmt.Fprintf(opts.out, "Applying %s... ", file.Name())
		if err := opts.executor(ctx, connectionString, file.SQL); err != nil {
			fmt.Fprintln(opts.out, "error")
			return fmt.Errorf("migration %s failed: %w", file.Name(), err)
		}
		fmt.Fprintln(opts.out, "ok")
	}

	fmt.Fprintln(opts.out)
	fmt.Fprintln(opts.out, "Migrations deployed!")
	return nil
}

func selectMigrationFiles(allMigrations []clidatabase.MigrationFile, target string, all bool) ([]clidatabase.MigrationFile, error) {
	if all {
		return allMigrations, nil
	}
	if file, ok := findMigrationFile(allMigrations, target); ok {
		return []clidatabase.MigrationFile{file}, nil
	}
	return nil, fmt.Errorf("migration %q not found in volcano/migrations/\navailable migrations: %s", target, formatMigrationNames(allMigrations))
}

// findMigrationFile matches target, a file name with or without .sql or a
// path ending in one, against files.
func findMigrationFile(files []clidatabase.MigrationFile, target string) (clidatabase.MigrationFile, bool) {
	targetName := normalizeTargetMigration(target)
	for _, file := range files {
		baseName := file.Name()
		nameWithoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))
		if nameWithoutExt == targetName || baseName == targetName {
			return file, true
		}
	}
	return clidatabase.MigrationFile{}, false
}

func normalizeTargetMigration(target string) string {
	name := filepath.Base(target)
	if strings.HasSuffix(strings.ToLower(name), ".sql") {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

func formatMigrationNames(files []clidatabase.MigrationFile) string {
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = file.Name()
	}
	return strings.Join(names, ", ")
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
