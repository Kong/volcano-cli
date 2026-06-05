package bucket

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type deleteOptions struct {
	deps cliruntime.Deps
	name string
	yes  bool
	in   io.Reader
	out  io.Writer
}

func newDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <bucket>",
		Short: "Delete a storage bucket",
		Long: `Delete a storage bucket.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), deleteOptions{
				deps: deps,
				name: strings.TrimSpace(args[0]),
				yes:  yes,
				in:   cmd.InOrStdin(),
				out:  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	if opts.name == "" {
		return errors.New("bucket name cannot be empty")
	}
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "storage bucket", opts.name)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := clistorage.NewService(opts.deps).DeleteBucket(ctx, opts.name); err != nil {
		return err
	}
	output.Success(opts.out, "Bucket '%s' deleted", opts.name)
	return nil
}
