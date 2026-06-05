package schedulers

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deleteOptions struct {
	deps        cliruntime.Deps
	function    string
	schedulerID string
	yes         bool
	in          io.Reader
	out         io.Writer
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <function> <scheduler-id>",
		Short: "Delete a function scheduler",
		Long: `Delete a scheduled invocation for a function.

Deleting a scheduler is permanent and removes its scheduler rows and run history.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), deleteOptions{
				deps:        deps,
				function:    strings.TrimSpace(args[0]),
				schedulerID: strings.TrimSpace(args[1]),
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
	schedulerID, err := uuid.Parse(opts.schedulerID)
	if err != nil {
		return fmt.Errorf("invalid scheduler id %q: %w", opts.schedulerID, err)
	}
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "function scheduler", opts.schedulerID)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := clifunction.NewService(opts.deps).DeleteScheduler(ctx, opts.function, schedulerID); err != nil {
		return err
	}

	output.Success(opts.out, "Deleted scheduler %s", schedulerID.String())
	return nil
}
