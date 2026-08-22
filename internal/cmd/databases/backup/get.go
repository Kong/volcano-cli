package backup

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps     cliruntime.Deps
	database string
	name     string
	out      io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <database> <backup>",
		Short: "Show one backup",
		Long: `Show one backup of a database.

A backup reports no size for the first few minutes after it is taken, until the
storage provider has costed it.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name, err := parseTarget(args)
			if err != nil {
				return err
			}
			return runGet(cmd.Context(), getOptions{
				deps:     deps,
				database: database,
				name:     name,
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	backup, err := clidatabase.NewService(opts.deps).GetBackup(ctx, opts.database, opts.name)
	if err != nil {
		return err
	}

	output.DatabaseBackup(opts.out, backup)
	return nil
}
