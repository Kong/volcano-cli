package schedulers

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps     cliruntime.Deps
	function string
	out      io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <function>",
		Short: "List schedulers for a durable function",
		Long:  "List the schedulers that start executions of one durable function.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), listOptions{
				deps:     deps,
				function: strings.TrimSpace(args[0]),
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func runList(ctx context.Context, opts listOptions) error {
	schedulers, err := clidurable.NewService(opts.deps).ListSchedulers(ctx, opts.function)
	if err != nil {
		return err
	}

	output.DurableSchedulers(opts.out, opts.function, schedulers)
	return nil
}
