package backup

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type createOptions struct {
	deps     cliruntime.Deps
	database string
	name     string
	out      io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "create <database> <backup>",
		Short: "Back up a database",
		Long: `Capture a database as it is now.

The backup is restorable as soon as this returns. How many backups a database
may keep, and how long they are kept, come from the project's plan.

Examples:
  ` + cliruntime.CommandPath(deps, "databases backups create app before_migration"),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, name, err := parseTarget(args)
			if err != nil {
				return err
			}
			return runCreate(cmd.Context(), createOptions{
				deps:     deps,
				database: database,
				name:     name,
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func runCreate(ctx context.Context, opts createOptions) error {
	backup, err := clidatabase.NewService(opts.deps).CreateBackup(ctx, opts.database, opts.name)
	if err != nil {
		return err
	}

	output.DatabaseBackupCreated(opts.out, backup, opts.database)
	return nil
}
