package bucket

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type listOptions struct {
	deps cliruntime.Deps
	out  io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List storage buckets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), listOptions{
				deps: deps,
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func runList(ctx context.Context, opts listOptions) error {
	buckets, err := clistorage.NewService(opts.deps).ListBuckets(ctx)
	if err != nil {
		return err
	}
	output.StorageBuckets(opts.out, buckets)
	return nil
}
