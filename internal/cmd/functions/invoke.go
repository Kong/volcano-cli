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
	deps            cliruntime.Deps
	functionOptions []clifunction.Option
	identifier      string
	functionID      string
	payload         string
	hasPayload      bool
	jsonOutput      bool
	out             io.Writer
}

func newInvoke(deps cliruntime.Deps, functionOptions ...clifunction.Option) *cobra.Command {
	opts := invokeOptions{}
	cmd := &cobra.Command{
		Use:   "invoke [name]",
		Short: "Invoke a function",
		Long: "Invoke a deployed function by alias, name, path, or ID.\n\n" +
			"In local mode this is a function-logic harness: the call runs as the " +
			"pre-provisioned local user and needs no credential. Exercise real " +
			"per-user auth (signed-in vs anonymous) and cross-service behavior " +
			"against a cloud deployment or the app-driven path instead.",
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, `functions invoke hello --payload '{"name":"Ada"}'`),
			cliruntime.CommandPath(deps, `functions invoke hello --json`),
			cliruntime.CommandPath(deps, `functions invoke --id 33333333-3333-4333-8333-333333333333`)),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.functionOptions = functionOptions
			if len(args) == 1 {
				opts.identifier = strings.TrimSpace(args[0])
			}
			opts.hasPayload = cmd.Flags().Changed("payload")
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

	var payload map[string]any
	var err error
	if opts.hasPayload {
		payload, err = loadInvokePayload(opts.payload)
		if err != nil {
			return err
		}
	}

	service := clifunction.NewService(opts.deps, opts.functionOptions...)
	var resp any
	if hasID {
		var functionID uuid.UUID
		functionID, err = uuid.Parse(strings.TrimSpace(opts.functionID))
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
		return map[string]any{}, nil
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
