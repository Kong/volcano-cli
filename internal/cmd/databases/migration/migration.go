package migration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient/common"
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
		Short: "Manage local database migrations",
		Long:  "Apply SQL migrations from the volcano/migrations directory to a local database.",
	}
	cmd.AddCommand(newDeploy(deps, executeSQLMigration))
	return cmd
}

func newWithExecutor(deps cliruntime.Deps, executor sqlExecutor) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migration",
		Short: "Manage database migrations",
		Long:  "Apply SQL migrations from the volcano/migrations directory.",
	}
	cmd.AddCommand(newUp(deps, executor))
	return cmd
}

func newUp(deps cliruntime.Deps, executor sqlExecutor) *cobra.Command {
	return newApply(deps, executor, "up", fmt.Sprintf(`Apply SQL migration files from volcano/migrations/ in alphabetical order.

Usage:
  %s
  %s
  %s
  %s

The --database flag is required. The CLI connects directly to your database via
pgproxy and executes the SQL files.

WARNING: Migrations are executed without tracking. You are responsible for not
re-running non-idempotent migrations.`,
		cliruntime.CommandPath(deps, "databases migration up --all -d mydb"),
		cliruntime.CommandPath(deps, "databases migration up -a -d mydb"),
		cliruntime.CommandPath(deps, "databases migration up -d mydb -f 001_create_users"),
		cliruntime.CommandPath(deps, "databases migration up --database mydb -f 001_create_users.sql")))
}

func newDeploy(deps cliruntime.Deps, executor sqlExecutor) *cobra.Command {
	return newApply(deps, executor, "deploy", fmt.Sprintf(`Apply SQL migration files from volcano/migrations/ in alphabetical order.

Usage:
  %s
  %s
  %s
  %s

The --database flag is required. The CLI connects directly to your database and
executes the SQL files.

WARNING: Migrations are executed without tracking. You are responsible for not
re-running non-idempotent migrations.`,
		cliruntime.CommandPath(deps, "migrations deploy --all -d mydb"),
		cliruntime.CommandPath(deps, "migrations deploy -a -d mydb"),
		cliruntime.CommandPath(deps, "migrations deploy -d mydb -f 001_create_users"),
		cliruntime.CommandPath(deps, "migrations deploy --database mydb -f 001_create_users.sql")))
}

func newApply(deps cliruntime.Deps, executor sqlExecutor, use, long string) *cobra.Command {
	var database string
	var file string
	var all bool
	cmd := &cobra.Command{
		Use:   use,
		Short: "Apply migrations",
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
	if opts.all && opts.file != "" {
		return errors.New("cannot use --all and --file together")
	}
	if !opts.all && opts.file == "" {
		return errors.New("specify either --all to apply all migrations or --file/-f to apply a specific migration")
	}

	migrationsDir := filepath.Join("volcano", "migrations")
	if opts.all {
		fmt.Fprintf(opts.out, "\nScanning %s...\n", migrationsDir)
	}

	allMigrations, err := scanMigrations(migrationsDir)
	if err != nil {
		return fmt.Errorf("failed to scan migrations: %w", err)
	}

	if len(allMigrations) == 0 {
		if opts.file != "" {
			return errors.New("no migration files found in volcano/migrations/\nnote: migrations must be placed in the volcano/migrations/ directory")
		}
		fmt.Fprintln(opts.out, "No migration files found in volcano/migrations/")
		return nil
	}

	migrationFiles, err := selectMigrationFiles(allMigrations, opts.file, opts.all)
	if err != nil {
		return err
	}

	if opts.all {
		fmt.Fprintf(opts.out, "Found %d file(s) (sorted alphabetically):\n", len(migrationFiles))
		for _, file := range migrationFiles {
			fmt.Fprintf(opts.out, "  - %s\n", filepath.Base(file))
		}
	} else {
		fmt.Fprintf(opts.out, "\nExecuting migration: %s\n", filepath.Base(migrationFiles[0]))
	}

	database, err := clidatabase.NewService(opts.deps).Get(ctx, opts.database)
	if err != nil {
		return err
	}
	if database.Status != common.DatabaseStatusActive {
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

	for _, sqlFile := range migrationFiles {
		fmt.Fprintf(opts.out, "Applying %s... ", filepath.Base(sqlFile))

		content, err := os.ReadFile(sqlFile)
		if err != nil {
			fmt.Fprintln(opts.out, "error")
			return fmt.Errorf("failed to read migration %s: %w", filepath.Base(sqlFile), err)
		}

		if err := opts.executor(ctx, connectionString, string(content)); err != nil {
			fmt.Fprintln(opts.out, "error")
			return fmt.Errorf("migration %s failed: %w", filepath.Base(sqlFile), err)
		}

		fmt.Fprintln(opts.out, "ok")
	}

	fmt.Fprintln(opts.out)
	fmt.Fprintln(opts.out, "Migrations deployed!")
	return nil
}

func selectMigrationFiles(allMigrations []string, target string, all bool) ([]string, error) {
	if all {
		return allMigrations, nil
	}

	targetName := normalizeTargetMigration(target)
	for _, file := range allMigrations {
		baseName := filepath.Base(file)
		nameWithoutExt := strings.TrimSuffix(baseName, filepath.Ext(baseName))
		if nameWithoutExt == targetName || baseName == targetName {
			return []string{file}, nil
		}
	}

	return nil, fmt.Errorf("migration %q not found in volcano/migrations/\navailable migrations: %s", target, formatMigrationNames(allMigrations))
}

func scanMigrations(migrationsDir string) ([]string, error) {
	entries, err := os.ReadDir(migrationsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".sql") {
			files = append(files, filepath.Join(migrationsDir, entry.Name()))
		}
	}

	sort.Strings(files)
	return files, nil
}

func normalizeTargetMigration(target string) string {
	name := filepath.Base(target)
	if strings.HasSuffix(strings.ToLower(name), ".sql") {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	return name
}

func formatMigrationNames(files []string) string {
	names := make([]string, len(files))
	for i, file := range files {
		names[i] = filepath.Base(file)
	}
	return strings.Join(names, ", ")
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
