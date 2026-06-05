package functions

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type runtimesOptions struct {
	deps cliruntime.Deps
	out  io.Writer
}

func newRuntimes(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "runtimes",
		Short: "List supported function runtimes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRuntimes(cmd.Context(), runtimesOptions{
				deps: deps,
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func runRuntimes(ctx context.Context, opts runtimesOptions) error {
	runtimes, err := clifunction.NewService(opts.deps).ListRuntimes(ctx)
	if err != nil {
		return err
	}

	output.FunctionRuntimes(opts.out, runtimes)
	return nil
}
