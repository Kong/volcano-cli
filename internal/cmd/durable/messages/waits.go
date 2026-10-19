package messages

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

type waitsOptions struct {
	deps        cliruntime.Deps
	function    string
	executionID uuid.UUID
	jsonOutput  bool
	out         io.Writer
}

func newWaits(deps cliruntime.Deps) *cobra.Command {
	opts := waitsOptions{}
	cmd := &cobra.Command{
		Use:   "waits <function> <execution-id>",
		Short: "Show the messages a durable execution is waiting for",
		Long: `List the waits an execution has open in ctx.waitForMessage, oldest first: the
message names it is waiting for, and when each wait times out.`,
		Example: "  " + cliruntime.CommandPath(deps,
			"durable messages waits order-pipeline 6f1c2a9e-3b7d-4c8a-9e2f-1a5b7c9d0e3f"),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			executionID, err := parseExecutionID(args[1])
			if err != nil {
				return err
			}
			opts.deps = deps
			opts.function = strings.TrimSpace(args[0])
			opts.executionID = executionID
			opts.out = cmd.OutOrStdout()
			return runWaits(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runWaits(ctx context.Context, opts waitsOptions) error {
	waits, err := clidurable.NewService(opts.deps).ListMessageWaits(ctx, opts.function, opts.executionID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, waits)
	}
	output.DurableMessageWaits(opts.out, opts.executionID.String(), waits)
	return nil
}
