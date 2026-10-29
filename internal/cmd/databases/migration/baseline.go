package migration

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/confirm"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newBaseline(deps cliruntime.Deps) *cobra.Command {
	var database string
	var names []string
	var all, yes, jsonOutput bool
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Record migrations as applied without running them",
		Long: fmt.Sprintf(`Record migration files in a database's migration history as applied, without
running them, so later runs skip them.

Use it once for a database whose schema already reflects its files, such as
one migrated with '%s' or an earlier --with-migrations deploy, before
its first tracked run. After a history was lost (history_missing), record
exactly the files that were already applied. A file recorded this way never
runs, so record only files the database already reflects.

Files the history already holds with the same content are skipped, so a
repeat records nothing. A file recorded with different content is refused
(checksum_mismatch), and nothing is recorded. A baseline does not wait for
another change to the database: while one is in progress it is refused with
database_busy, and you can try again.

By default this command prompts for confirmation. Use --yes to skip the prompt.

Usage:
  %s
  %s`,
			cliruntime.CommandPath(deps, "databases migration up"),
			cliruntime.CommandPath(deps, "databases migration baseline -d mydb --all"),
			cliruntime.CommandPath(deps, "databases migration baseline -d mydb -f 001_create_users -f 002_add_posts")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := requireDatabase(database)
			if err != nil {
				return err
			}
			c := commands{deps: deps, database: name}
			migrations, err := clidatabase.ReadMigrations(clidatabase.MigrationsDir)
			if err != nil {
				return err
			}
			files, err := selectBaselineFiles(migrations.For(name), migrations.Location(name), names, all)
			if err != nil {
				return err
			}
			if err := clidatabase.CheckTrackedNames(files); err != nil {
				return err
			}
			if err := clidatabase.NewService(deps).RequireMigrationRuns(cmd.Context()); err != nil {
				return baselineUnavailable(err)
			}
			if !yes {
				listed := make([]string, len(files))
				for i, file := range files {
					listed[i] = "  - " + file.Name()
				}
				// With --json, stdout carries only the JSON document.
				prompt := cmd.OutOrStdout()
				if jsonOutput {
					prompt = cmd.ErrOrStderr()
				}
				confirmed, err := confirm.Action(cmd.InOrStdin(), prompt,
					fmt.Sprintf("A baseline records these files in the migration history of database '%s' as applied, "+
						"without running them. A file recorded this way never runs.\n%s", name, strings.Join(listed, "\n")),
					fmt.Sprintf("Record %d file(s) as applied?", len(files)))
				if err != nil {
					return err
				}
				if !confirmed {
					return nil
				}
			}
			baseline, err := clidatabase.NewService(deps).BaselineMigrations(cmd.Context(), name, files)
			if err != nil {
				if errors.Is(err, clidatabase.ErrMigrationRunsUnavailable) {
					return baselineUnavailable(err)
				}
				return c.explain(err)
			}
			if jsonOutput {
				return writeJSON(cmd.OutOrStdout(), baseline)
			}
			output.DatabaseMigrationBaseline(cmd.OutOrStdout(), baseline, name)
			return nil
		},
	}
	databaseFlag(cmd, &database)
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Record every migration file for the database")
	cmd.Flags().StringArrayVarP(&names, "file", "f", nil, "Record a migration by name or path; repeat for several")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	cmd.MarkFlagsMutuallyExclusive("all", "file")
	cmd.MarkFlagsOneRequired("all", "file")
	return cmd
}

// baselineUnavailable words an API without tracked runs for a baseline,
// which has no untracked counterpart to suggest.
func baselineUnavailable(err error) error {
	if !errors.Is(err, clidatabase.ErrMigrationRunsUnavailable) {
		return err
	}
	return api.WithCause(fmt.Errorf("%w (capability %q), so there is no migration history to record a baseline in",
		clidatabase.ErrMigrationRunsUnavailable, clidatabase.MigrationsCapability), err)
}

// selectBaselineFiles picks the files a baseline records: all of them, or
// the ones named, each of which has to exist.
func selectBaselineFiles(files []clidatabase.MigrationFile, location string, names []string, all bool) ([]clidatabase.MigrationFile, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no migration files found in %s/", location)
	}
	if all {
		return files, nil
	}
	var selected []clidatabase.MigrationFile
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, errors.New("--file needs a migration name")
		}
		file, ok := findMigrationFile(files, name)
		if !ok {
			return nil, fmt.Errorf("migration %q not found in %s/\navailable migrations: %s", name, location, formatMigrationNames(files))
		}
		if !seen[file.Path] {
			seen[file.Path] = true
			selected = append(selected, file)
		}
	}
	return selected, nil
}
