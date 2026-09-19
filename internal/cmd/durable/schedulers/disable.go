package schedulers

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type disableOptions struct {
	deps        cliruntime.Deps
	function    string
	schedulerID uuid.UUID
	out         io.Writer
}

func newDisable(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <function> <scheduler-id>",
		Short: "Disable a durable function scheduler",
		Long: `Stop a scheduler's ticks without deleting it.

Executions the scheduler already started keep running; stop them with
'durable executions stop' if you need them ended.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			schedulerID, err := parseSchedulerID(args[1])
			if err != nil {
				return err
			}
			return runDisable(cmd.Context(), disableOptions{
				deps:        deps,
				function:    strings.TrimSpace(args[0]),
				schedulerID: schedulerID,
				out:         cmd.OutOrStdout(),
			})
		},
	}
}

func runDisable(ctx context.Context, opts disableOptions) error {
	scheduler, err := clidurable.NewService(opts.deps).DisableScheduler(ctx, opts.function, opts.schedulerID)
	if err != nil {
		return err
	}

	output.Scheduler(opts.out, scheduler)
	output.Success(opts.out, "Disabled scheduler %s", opts.schedulerID.String())
	return nil
}

// parseSchedulerID keeps the id check in the command, so a typo fails before
// anything is sent.
func parseSchedulerID(value string) (uuid.UUID, error) {
	trimmed := strings.TrimSpace(value)
	schedulerID, err := uuid.Parse(trimmed)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid scheduler id %q: %w", trimmed, err)
	}
	return schedulerID, nil
}
