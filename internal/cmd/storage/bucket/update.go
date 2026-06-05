package bucket

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clistorage "github.com/Kong/volcano-cli/internal/storage"
)

type updateOptions struct {
	deps              cliruntime.Deps
	name              string
	allowedMimeTypes  []string
	mimeTypesProvided bool
	fileSizeLimit     int64
	limitProvided     bool
	out               io.Writer
}

func newUpdate(deps cliruntime.Deps) *cobra.Command {
	var allowed []string
	var sizeLimit int64
	cmd := &cobra.Command{
		Use:   "update <bucket>",
		Short: "Update a storage bucket",
		Long: `Update a storage bucket's configuration.

Use --allowed-mime-type to set the allowed MIME types (repeat the flag for
multiple values). Passing the flag with an empty value sends an empty list,
which the server treats as "disallow all uploads" rather than "allow any";
omit the flag instead if you do not want to change the allowed MIME types.
Use --file-size-limit to set the maximum object size in bytes.
Fields you do not pass are left unchanged.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("allowed-mime-type") && !cmd.Flags().Changed("file-size-limit") {
				return errors.New("at least one of --allowed-mime-type or --file-size-limit is required")
			}
			return runUpdate(cmd.Context(), updateOptions{
				deps:              deps,
				name:              strings.TrimSpace(args[0]),
				allowedMimeTypes:  allowed,
				mimeTypesProvided: cmd.Flags().Changed("allowed-mime-type"),
				fileSizeLimit:     sizeLimit,
				limitProvided:     cmd.Flags().Changed("file-size-limit"),
				out:               cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringSliceVar(&allowed, "allowed-mime-type", nil, "Allowed MIME type (repeatable; empty value clears the list and disallows all uploads)")
	cmd.Flags().Int64Var(&sizeLimit, "file-size-limit", 0, "Maximum object size in bytes")
	return cmd
}

func runUpdate(ctx context.Context, opts updateOptions) error {
	if opts.name == "" {
		return errors.New("bucket name cannot be empty")
	}
	input := api.StorageBucketUpdateInput{}
	if opts.mimeTypesProvided {
		mimes := append([]string(nil), opts.allowedMimeTypes...)
		input.AllowedMimeTypes = &mimes
	}
	if opts.limitProvided {
		limit := opts.fileSizeLimit
		input.FileSizeLimit = &limit
	}

	bucket, err := clistorage.NewService(opts.deps).UpdateBucket(ctx, opts.name, input)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Bucket '%s' updated", bucket.Name)
	output.StorageBucket(opts.out, bucket)
	return nil
}
