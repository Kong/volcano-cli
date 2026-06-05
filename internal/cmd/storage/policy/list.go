package policy

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

type listOptions struct {
	deps   cliruntime.Deps
	bucket string
	out    io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <bucket>",
		Short: "List policies attached to a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), listOptions{
				deps:   deps,
				bucket: strings.TrimSpace(args[0]),
				out:    cmd.OutOrStdout(),
			})
		},
	}
}

func runList(ctx context.Context, opts listOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	policies, err := clistorage.NewService(opts.deps).ListPolicies(ctx, opts.bucket)
	if err != nil {
		return err
	}
	output.StoragePolicies(opts.out, opts.bucket, policies)
	return nil
}
