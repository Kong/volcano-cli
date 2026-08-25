package backup

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps     cliruntime.Deps
	database string
	out      io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <database>",
		Short: "List a database's backups",
		Long: `List every backup of a database, newest first.

Backups you took and backups the schedule produced are both listed; the source
column tells them apart. The list also reports the window a point-in-time
restore may target.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := parseDatabase(args)
			if err != nil {
				return err
			}
			return runList(cmd.Context(), listOptions{
				deps:     deps,
				database: database,
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func runList(ctx context.Context, opts listOptions) error {
	backups, err := clidatabase.NewService(opts.deps).ListBackups(ctx, opts.database)
	if err != nil {
		return err
	}

	output.DatabaseBackups(opts.out, backups, opts.database)
	return nil
}
