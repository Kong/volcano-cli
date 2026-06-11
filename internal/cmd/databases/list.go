package databases

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	clidatabase "github.com/Kong/volcano-cli/internal/database"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps                 cliruntime.Deps
	page                 int
	limit                int
	showConnectionString bool
	out                  io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	var page int
	var limit int
	var showConnectionString bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List cloud databases",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runList(cmd.Context(), listOptions{
				deps:                 deps,
				page:                 page,
				limit:                limit,
				showConnectionString: showConnectionString,
				out:                  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().IntVar(&page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&limit, "limit", api.DefaultLimit, "Number of databases per page")
	cmd.Flags().BoolVar(&showConnectionString, "show-connection-string", false, "Show database connection strings")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	databases, err := clidatabase.NewService(opts.deps).ListPage(ctx, opts.page, opts.limit)
	if err != nil {
		return err
	}

	output.Databases(opts.out, databases, opts.showConnectionString, cliruntime.CommandPath(opts.deps, ""))
	return nil
}
