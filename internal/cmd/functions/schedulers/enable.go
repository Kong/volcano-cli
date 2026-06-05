package schedulers

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type enableOptions struct {
	deps        cliruntime.Deps
	function    string
	schedulerID string
	out         io.Writer
}

func newEnable(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "enable <function> <scheduler-id>",
		Short: "Enable a function scheduler",
		Long:  "Re-enable a previously disabled scheduled invocation.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEnable(cmd.Context(), enableOptions{
				deps:        deps,
				function:    strings.TrimSpace(args[0]),
				schedulerID: strings.TrimSpace(args[1]),
				out:         cmd.OutOrStdout(),
			})
		},
	}
}

func runEnable(ctx context.Context, opts enableOptions) error {
	schedulerID, err := uuid.Parse(opts.schedulerID)
	if err != nil {
		return fmt.Errorf("invalid scheduler id %q: %w", opts.schedulerID, err)
	}

	scheduler, err := clifunction.NewService(opts.deps).EnableScheduler(ctx, opts.function, schedulerID)
	if err != nil {
		return err
	}

	output.Scheduler(opts.out, scheduler)
	output.Success(opts.out, "Enabled scheduler %s", schedulerID.String())
	return nil
}
