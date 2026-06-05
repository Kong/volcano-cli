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

type getOptions struct {
	deps       cliruntime.Deps
	bucket     string
	identifier string
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <bucket> <name-or-id>",
		Short: "Get one policy by name or ID",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd.Context(), getOptions{
				deps:       deps,
				bucket:     strings.TrimSpace(args[0]),
				identifier: strings.TrimSpace(args[1]),
				out:        cmd.OutOrStdout(),
			})
		},
	}
}

func runGet(ctx context.Context, opts getOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	if opts.identifier == "" {
		return errors.New("policy identifier cannot be empty")
	}
	policy, err := clistorage.NewService(opts.deps).GetPolicy(ctx, opts.bucket, opts.identifier)
	if err != nil {
		return err
	}
	output.StoragePolicy(opts.out, opts.bucket, policy)
	return nil
}
