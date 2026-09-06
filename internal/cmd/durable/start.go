package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type startOptions struct {
	deps     cliruntime.Deps
	function string
	input    string
	name     string
	out      io.Writer
}

func newStart(deps cliruntime.Deps) *cobra.Command {
	opts := startOptions{}
	cmd := &cobra.Command{
		Use:   "start <function>",
		Short: "Start a durable execution",
		Long: `Start one execution of a durable function.

Starting is asynchronous by construction: an execution can run for hours, so
this returns a handle rather than a result. Read the execution to follow it.

--name is the execution's idempotency key. Repeating a start under a name that
already names an execution returns that one instead of beginning a second, and
is not charged again, which is what makes retrying a start safe. Omit it and
Volcano generates one.

--input accepts inline JSON or a path to a JSON file, and is the function's
input verbatim.`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "durable start order-pipeline"),
			cliruntime.CommandPath(deps, `durable start order-pipeline --input '{"order_id":4417}'`),
			cliruntime.CommandPath(deps, "durable start order-pipeline --input order.json --name order-4417")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.function = strings.TrimSpace(args[0])
			opts.out = cmd.OutOrStdout()
			return runStart(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.input, "input", "", "Inline JSON object or path to a JSON file passed as the execution's input")
	cmd.Flags().StringVar(&opts.name, "name", "", "Execution name, used as the idempotency key")
	return cmd
}

func runStart(ctx context.Context, opts startOptions) error {
	start := api.DurableExecutionStartInput{Name: strings.TrimSpace(opts.name)}
	// An omitted --input starts the execution with no input at all, which is not
	// the same thing as starting it with an empty object.
	if value := strings.TrimSpace(opts.input); value != "" {
		input, err := parseStartInput(value)
		if err != nil {
			return err
		}
		start.Input = input
	}

	execution, err := clidurable.NewService(opts.deps).StartExecution(ctx, opts.function, start)
	if err != nil {
		return err
	}

	output.DurableExecution(opts.out, execution)
	output.Success(opts.out, "Execution %s started", execution.Name)
	fmt.Fprintf(opts.out, "Follow it with %s\n",
		cliruntime.CommandPath(opts.deps, "durable executions get "+opts.function+" "+execution.Id.String()))
	return nil
}

// parseStartInput reads --input as inline JSON, or as the contents of the file
// it names.
func parseStartInput(value string) (map[string]any, error) {
	data := []byte(value)
	if info, err := os.Stat(value); err == nil && !info.IsDir() {
		fileBytes, readErr := os.ReadFile(value)
		if readErr != nil {
			return nil, fmt.Errorf("failed to read input file %q: %w", value, readErr)
		}
		data = fileBytes
	}

	var input map[string]any
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("input must be a JSON object: %w", err)
	}
	return input, nil
}
