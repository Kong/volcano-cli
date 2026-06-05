package bucket

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type getOptions struct {
	deps cliruntime.Deps
	name string
	out  io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <bucket>",
		Short: "Get a storage bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), getOptions{
				deps: deps,
				name: strings.TrimSpace(args[0]),
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	if opts.name == "" {
		return errors.New("bucket name cannot be empty")
	}
	bucket, err := clistorage.NewService(opts.deps).GetBucket(ctx, opts.name)
	if err != nil {
		return err
	}
	output.StorageBucket(opts.out, bucket)
	return nil
}
