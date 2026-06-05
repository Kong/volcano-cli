package object

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type deleteOptions struct {
	deps           cliruntime.Deps
	serviceOptions []clistorage.Option
	bucket         string
	remotePath     string
	yes            bool
	in             io.Reader
	out            io.Writer
}

func newDelete(deps cliruntime.Deps, serviceOptions ...clistorage.Option) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <bucket> <remote-path>",
		Short: "Delete an object from a bucket",
		Long: `Delete an object from a bucket.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDelete(cmd.Context(), deleteOptions{
				deps:           deps,
				serviceOptions: serviceOptions,
				bucket:         strings.TrimSpace(args[0]),
				remotePath:     strings.TrimSpace(args[1]),
				yes:            yes,
				in:             cmd.InOrStdin(),
				out:            cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDelete(ctx context.Context, opts deleteOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	if opts.remotePath == "" {
		return errors.New("remote path cannot be empty")
	}
	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "storage object", fmt.Sprintf("%s in bucket %s", opts.remotePath, opts.bucket))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := clistorage.NewService(opts.deps, opts.serviceOptions...).DeleteObject(ctx, opts.bucket, opts.remotePath); err != nil {
		return err
	}
	output.Success(opts.out, "Object '%s' deleted from bucket '%s'", opts.remotePath, opts.bucket)
	return nil
}
