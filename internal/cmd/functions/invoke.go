package functions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	clifunction "github.com/Kong/volcano-cli/internal/function"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type invokeOptions struct {
	deps       cliruntime.Deps
	identifier string
	functionID string
	payload    string
	jsonOutput bool
	out        io.Writer
}

func newInvoke(deps cliruntime.Deps) *cobra.Command {
	opts := invokeOptions{}
	cmd := &cobra.Command{
		Use:   "invoke [name]",
		Short: "Invoke a function",
		Long:  "Invoke a deployed function by alias, name, path, or ID.",
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, `functions invoke hello --payload '{"name":"Ada"}'`),
			cliruntime.CommandPath(deps, `functions invoke hello --json`),
			cliruntime.CommandPath(deps, `functions invoke --id 33333333-3333-4333-8333-333333333333`)),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			if len(args) == 1 {
				opts.identifier = strings.TrimSpace(args[0])
			}
			opts.out = cmd.OutOrStdout()
			return runInvoke(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.functionID, "id", "", "Function ID to invoke directly")
	cmd.Flags().StringVar(&opts.payload, "payload", "", "Inline JSON object passed as the invocation payload")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print compact JSON output")
	return cmd
}

func runInvoke(ctx context.Context, opts invokeOptions) error {
	hasName := strings.TrimSpace(opts.identifier) != ""
	hasID := strings.TrimSpace(opts.functionID) != ""
	switch {
	case hasName && hasID:
		return errors.New("specify either a function name or --id, not both")
	case !hasName && !hasID:
		return errors.New("specify a function name or --id")
	}

	payload, err := loadInvokePayload(opts.payload)
	if err != nil {
		return err
	}

	service := clifunction.NewService(opts.deps)
	var resp any
	if hasID {
		functionID, err := uuid.Parse(strings.TrimSpace(opts.functionID))
		if err != nil {
			return fmt.Errorf("invalid function ID %q: %w", opts.functionID, err)
		}
		resp, err = service.InvokeByID(ctx, functionID, payload)
		if err != nil {
			return err
		}
	} else {
		resp, err = service.Invoke(ctx, opts.identifier, payload)
		if err != nil {
			return err
		}
	}

	return writeInvocationResponse(opts.out, resp, opts.jsonOutput)
}

func loadInvokePayload(value string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(value), &payload); err != nil {
		return nil, fmt.Errorf("payload must be a JSON object: %w", err)
	}
	if payload == nil {
		return nil, errors.New("payload must be a JSON object")
	}
	return payload, nil
}

func writeInvocationResponse(w io.Writer, resp any, compact bool) error {
	encoder := json.NewEncoder(w)
	if !compact {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(resp)
}
