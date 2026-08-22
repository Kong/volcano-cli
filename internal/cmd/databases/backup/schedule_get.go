package backup

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type scheduleGetOptions struct {
	deps     cliruntime.Deps
	database string
	out      io.Writer
}

func newScheduleGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <database>",
		Short: "Show a database's backup schedule",
		Long:  "Show when a database is backed up automatically, and how long each backup is kept.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			database, err := parseDatabase(args)
			if err != nil {
				return err
			}
			return runScheduleGet(cmd.Context(), scheduleGetOptions{
				deps:     deps,
				database: database,
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func runScheduleGet(ctx context.Context, opts scheduleGetOptions) error {
	schedule, err := clidatabase.NewService(opts.deps).GetBackupSchedule(ctx, opts.database)
	if err != nil {
		return err
	}

	output.DatabaseBackupSchedule(opts.out, schedule, opts.database)
	return nil
}
