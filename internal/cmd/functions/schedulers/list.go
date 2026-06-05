package schedulers

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	clifunction "github.com/Kong/volcano-cli/internal/function"
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
		Short: "List schedulers for a function",
		Long:  "List scheduled invocations configured for a deployed cloud function.",
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
	fn, resp, err := clifunction.NewService(opts.deps).ListSchedulers(ctx, opts.function)
	if err != nil {
		return err
	}
	output.Schedulers(opts.out, fn, resp)
	return nil
}
