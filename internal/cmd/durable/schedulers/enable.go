package schedulers

import (
	"context"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type enableOptions struct {
	deps        cliruntime.Deps
	function    string
	schedulerID uuid.UUID
	out         io.Writer
}

func newEnable(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "enable <function> <scheduler-id>",
		Short: "Enable a durable function scheduler",
		Long:  "Resume a scheduler's ticks. The next tick starts an execution on schedule.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			schedulerID, err := parseSchedulerID(args[1])
			if err != nil {
				return err
			}
			return runEnable(cmd.Context(), enableOptions{
				deps:        deps,
				function:    strings.TrimSpace(args[0]),
				schedulerID: schedulerID,
				out:         cmd.OutOrStdout(),
			})
		},
	}
}

func runEnable(ctx context.Context, opts enableOptions) error {
	scheduler, err := clidurable.NewService(opts.deps).EnableScheduler(ctx, opts.function, opts.schedulerID)
	if err != nil {
		return err
	}

	output.Scheduler(opts.out, scheduler)
	output.Success(opts.out, "Enabled scheduler %s", opts.schedulerID.String())
	return nil
}
