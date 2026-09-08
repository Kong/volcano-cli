package durable

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps       cliruntime.Deps
	identifier string
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get a durable function",
		Long: `Show one durable function, including the execution limits it was created with.

Those limits come from the project's plan and are fixed for the life of the
function; changing them means creating a new one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), getOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				out:        cmd.OutOrStdout(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	function, err := clidurable.NewService(opts.deps).Get(ctx, opts.identifier)
	if err != nil {
		return err
	}

	output.DurableFunction(opts.out, function)
	return nil
}
