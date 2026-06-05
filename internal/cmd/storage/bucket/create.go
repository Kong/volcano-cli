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

type createOptions struct {
	deps             cliruntime.Deps
	name             string
	allowedMimeTypes []string
	fileSizeLimit    int64
	limitProvided    bool
	out              io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	var allowed []string
	var sizeLimit int64
	cmd := &cobra.Command{
		Use:   "create <bucket>",
		Short: "Create a storage bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCreate(cmd.Context(), createOptions{
				deps:             deps,
				name:             strings.TrimSpace(args[0]),
				allowedMimeTypes: allowed,
				fileSizeLimit:    sizeLimit,
				limitProvided:    cmd.Flags().Changed("file-size-limit"),
				out:              cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringSliceVar(&allowed, "allowed-mime-type", nil, "Allowed MIME type (repeatable)")
	cmd.Flags().Int64Var(&sizeLimit, "file-size-limit", 0, "Maximum object size in bytes")
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	if opts.name == "" {
		return errors.New("bucket name cannot be empty")
	}
	input := api.StorageBucketCreateInput{
		Name:             opts.name,
		AllowedMimeTypes: opts.allowedMimeTypes,
	}
	if opts.limitProvided {
		limit := opts.fileSizeLimit
		input.FileSizeLimit = &limit
	}

	bucket, err := clistorage.NewService(opts.deps).CreateBucket(ctx, input)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Bucket '%s' created", bucket.Name)
	output.StorageBucket(opts.out, bucket)
	return nil
}
