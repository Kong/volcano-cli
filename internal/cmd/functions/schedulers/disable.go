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

type disableOptions struct {
	deps        cliruntime.Deps
	function    string
	schedulerID string
	out         io.Writer
}

func newDisable(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <function> <scheduler-id>",
		Short: "Disable a function scheduler",
		Long:  "Disable a scheduled invocation without deleting its scheduler record.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDisable(cmd.Context(), disableOptions{
				deps:        deps,
				function:    strings.TrimSpace(args[0]),
				schedulerID: strings.TrimSpace(args[1]),
				out:         cmd.OutOrStdout(),
			})
		},
	}
}

func runDisable(ctx context.Context, opts disableOptions) error {
	schedulerID, err := uuid.Parse(opts.schedulerID)
	if err != nil {
		return fmt.Errorf("invalid scheduler id %q: %w", opts.schedulerID, err)
	}

	scheduler, err := clifunction.NewService(opts.deps).DisableScheduler(ctx, opts.function, schedulerID)
	if err != nil {
		return err
	}

	output.Scheduler(opts.out, scheduler)
	output.Success(opts.out, "Disabled scheduler %s", schedulerID.String())
	return nil
}
