package executions

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

type getOptions struct {
	deps        cliruntime.Deps
	function    string
	executionID uuid.UUID
	out         io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <function> <execution-id>",
		Short: "Get one durable execution",
		Long: `Show one execution, including its result once it has finished.

This is the command to poll: reading an execution is what refreshes its status,
and a result is read straight through from the execution rather than stored, so
it is available for as long as the function's retention covers it.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			executionID, err := parseExecutionID(args[1])
			if err != nil {
				return err
			}
			return runGet(cmd.Context(), getOptions{
				deps:        deps,
				function:    strings.TrimSpace(args[0]),
				executionID: executionID,
				out:         cmd.OutOrStdout(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	execution, err := clidurable.NewService(opts.deps).GetExecution(ctx, opts.function, opts.executionID)
	if err != nil {
		return err
	}

	output.DurableExecution(opts.out, execution)
	return nil
}

// parseExecutionID rejects a name here rather than sending it to a route that
// takes an id: an execution is addressed by id, while its name is only an
// idempotency key and is not unique over time.
func parseExecutionID(value string) (uuid.UUID, error) {
	executionID, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid execution ID %q: %w", value, err)
	}
	return executionID, nil
}
