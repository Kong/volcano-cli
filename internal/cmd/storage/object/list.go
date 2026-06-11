package object

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

type listOptions struct {
	deps           cliruntime.Deps
	serviceOptions []clistorage.Option
	bucket         string
	prefix         string
	limit          int
	cursor         string
	out            io.Writer
}

func newList(deps cliruntime.Deps, serviceOptions ...clistorage.Option) *cobra.Command {
	var prefix string
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "list <bucket>",
		Short: "List objects in a bucket",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd.Context(), listOptions{
				deps:           deps,
				serviceOptions: serviceOptions,
				bucket:         strings.TrimSpace(args[0]),
				prefix:         prefix,
				limit:          limit,
				cursor:         cursor,
				out:            cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "Filter objects by path prefix")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum objects to return (server default if 0)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a prior page")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	if opts.bucket == "" {
		return errors.New("bucket name cannot be empty")
	}
	page, err := clistorage.NewService(opts.deps, opts.serviceOptions...).ListObjects(ctx, opts.bucket, api.StorageObjectListOptions{
		Prefix: opts.prefix,
		Limit:  opts.limit,
		Cursor: opts.cursor,
	})
	if err != nil {
		return err
	}
	output.StorageObjects(opts.out, opts.bucket, page, cliruntime.CommandPath(opts.deps, ""))
	return nil
}
