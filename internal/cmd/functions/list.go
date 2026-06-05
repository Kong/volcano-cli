package functions

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	clifunction "github.com/Kong/volcano-cli/internal/function"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps  cliruntime.Deps
	page  int
	limit int
	out   io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	var page int
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cloud functions",
		Long:  "List cloud functions for the current project.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), listOptions{
				deps:  deps,
				page:  page,
				limit: limit,
				out:   cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&limit, "limit", api.DefaultLimit, "Number of functions per page")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	functions, err := clifunction.NewService(opts.deps).ListPage(ctx, opts.page, opts.limit)
	if err != nil {
		return err
	}

	output.Functions(opts.out, functions)
	return nil
}
