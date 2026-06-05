// Package object provides storage object commands.
package object

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

type copyOptions struct {
	deps           cliruntime.Deps
	serviceOptions []clistorage.Option
	bucket         string
	from           string
	to             string
	out            io.Writer
}

func newCopy(deps cliruntime.Deps, serviceOptions ...clistorage.Option) *cobra.Command {
	return &cobra.Command{
		Use:   "copy <bucket> <source-path> <dest-path>",
		Short: "Copy an object within a bucket",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCopy(cmd.Context(), copyOptions{
				deps:           deps,
				serviceOptions: serviceOptions,
				bucket:         strings.TrimSpace(args[0]),
				from:           strings.TrimSpace(args[1]),
				to:             strings.TrimSpace(args[2]),
				out:            cmd.OutOrStdout(),
			})
		},
	}
}

func runCopy(ctx context.Context, opts copyOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	if opts.from == "" {
		return errors.New("source path cannot be empty")
	}
	if opts.to == "" {
		return errors.New("destination path cannot be empty")
	}
	object, err := clistorage.NewService(opts.deps, opts.serviceOptions...).CopyObject(ctx, opts.bucket, opts.from, opts.to)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Object copied to '%s' in bucket '%s'", object.Name, opts.bucket)
	output.StorageObject(opts.out, opts.bucket, object)
	return nil
}
