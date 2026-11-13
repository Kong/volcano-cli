package migration

import (
	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newRuns(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Inspect tracked migration runs",
		Long: `Inspect a database's tracked migration runs: the ones 'migration apply' and
--with-migrations deploys start, and the ones Git deploys start. Runs stay
readable whether or not tracked runs are available.`,
	}
	cmd.AddCommand(newRunsList(deps))
	cmd.AddCommand(newRunsGet(deps))
	return cmd
}

func newRunsList(deps cliruntime.Deps) *cobra.Command {
	var database string
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a database's recent migration runs",
		Long: `List a database's 50 most recent migration runs, newest first. Get a run for
its files.

Usage:
  ` + cliruntime.CommandPath(deps, "databases migration runs list -d mydb"),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			name, err := requireDatabase(database)
			if err != nil {
				return err
			}
			runs, err := clidatabase.NewService(deps).MigrationRuns(cmd.Context(), name)
			if err != nil {
				return err
			}
			if jsonOutput {
				return writeJSON(cmd.OutOrStdout(), runs)
			}
			output.DatabaseMigrationRuns(cmd.OutOrStdout(), runs, name)
			return nil
		},
	}
	databaseFlag(cmd, &database)
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}
