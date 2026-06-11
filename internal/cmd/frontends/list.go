package frontends

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
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
		Short: "List frontends",
		Long:  "List frontends for the current project.",
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
	cmd.Flags().IntVar(&limit, "limit", api.DefaultLimit, "Number of frontends per page")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	frontends, err := clifrontend.NewService(opts.deps).ListPage(ctx, opts.page, opts.limit)
	if err != nil {
		return err
	}

	output.Frontends(opts.out, frontends, cliruntime.CommandPath(opts.deps, ""))
	return nil
}
