package migration

import (
	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newStatus(deps cliruntime.Deps) *cobra.Command {
	var database string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Compare migration files with a database's history",
		Long: `Show where each migration file for a database stands against the database's
migration history:

  applied     a tracked run applied it
  baselined   a baseline recorded it without running it
  pending     the history does not record it; the next run applies it
  changed     its content differs from what the history recorded; a run
              refuses it until the recorded content is back
  missing     the history records it, but it is not in volcano/migrations

Reading the history opens a connection to the database, which wakes it if it
is suspended. It works whether or not tracked migration runs are available.

Usage:
  ` + cliruntime.CommandPath(deps, "databases migration status -d mydb"),
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
			history, err := clidatabase.NewService(deps).MigrationHistory(cmd.Context(), name)
			if err != nil {
				return c.explain(err)
			}
			status := clidatabase.CompareMigrations(name, history, migrations.For(name))
			if jsonOutput {
				return writeJSON(cmd.OutOrStdout(), status)
			}
			output.DatabaseMigrationStatus(cmd.OutOrStdout(), status, migrations.Location(name), c.output())
			return nil
		},
	}
	databaseFlag(cmd, &database)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}
