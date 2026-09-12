package schedulers

import (
	"context"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deleteOptions struct {
	deps        cliruntime.Deps
	function    string
	schedulerID uuid.UUID
	yes         bool
	in          io.Reader
	out         io.Writer
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <function> <scheduler-id>",
		Short: "Delete a durable function scheduler",
		Long: `Delete a scheduler and its run history.

Executions the scheduler already started keep running, and stay readable for as
long as the function's retention allows.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			schedulerID, err := parseSchedulerID(args[1])
			if err != nil {
				return err
			}
			return runDelete(cmd.Context(), deleteOptions{
				deps:        deps,
				function:    strings.TrimSpace(args[0]),
				schedulerID: schedulerID,
				yes:         yes,
				in:          cmd.InOrStdin(),
				out:         cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "durable function scheduler", opts.schedulerID.String())
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := clidurable.NewService(opts.deps).DeleteScheduler(ctx, opts.function, opts.schedulerID); err != nil {
		return err
	}

	output.Success(opts.out, "Deleted scheduler %s", opts.schedulerID.String())
	return nil
}
