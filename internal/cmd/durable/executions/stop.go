package executions

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

type stopOptions struct {
	deps        cliruntime.Deps
	function    string
	executionID uuid.UUID
	yes         bool
	in          io.Reader
	out         io.Writer
}

func newStop(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "stop <function> <execution-id>",
		Short: "Stop a running durable execution",
		Long: `Stop one execution of a durable function.

Steps already completed are not undone, and work already in flight is not
interrupted mid-attempt: the execution stops at its next checkpoint. Stopping
one that has already finished reports the state it is in rather than failing,
so a retried stop is safe.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			executionID, err := parseExecutionID(args[1])
			if err != nil {
				return err
			}
			return runStop(cmd.Context(), stopOptions{
				deps:        deps,
				function:    strings.TrimSpace(args[0]),
				executionID: executionID,
				yes:         yes,
				in:          cmd.InOrStdin(),
				out:         cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runStop(ctx context.Context, opts stopOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Action(opts.in, opts.out,
			"Stopping an execution ends it where it is; steps already completed are not undone.",
			"Stop execution "+opts.executionID.String()+"?")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	execution, err := clidurable.NewService(opts.deps).StopExecution(ctx, opts.function, opts.executionID)
	if err != nil {
		return err
	}

	output.DurableExecution(opts.out, execution)
	output.Success(opts.out, "Stop requested for execution %s", execution.Id.String())
	return nil
}
