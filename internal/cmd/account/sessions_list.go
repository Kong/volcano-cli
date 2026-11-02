package account

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliaccount "github.com/Kong/volcano-cli/internal/account"
	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type sessionsListOptions struct {
	deps       cliruntime.Deps
	page       int
	limit      int
	jsonOutput bool
	out        io.Writer
}

func newSessionsList(deps cliruntime.Deps) *cobra.Command {
	opts := sessionsListOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your account's sessions",
		Long: `List the sessions signed in to your account, browser and CLI alike, with the
device, address, and last activity of each. The session this CLI is logged in
with is marked "this CLI".`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.out = cmd.OutOrStdout()
			return runSessionsList(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&opts.limit, "limit", api.DefaultLimit, "Number of sessions per page")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runSessionsList(ctx context.Context, opts sessionsListOptions) error {
	page, err := cliaccount.NewService(opts.deps).Sessions(ctx, opts.page, opts.limit)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, page)
	}
	next := ""
	if page.Page < page.TotalPages {
		next = cliruntime.CommandPath(opts.deps,
			fmt.Sprintf("account sessions list --page %d --limit %d", page.Page+1, page.Limit))
	}
	output.AccountSessions(opts.out, output.AccountSessionsPage{
		Sessions:    page.Sessions,
		Page:        page.Page,
		Limit:       page.Limit,
		Total:       page.Total,
		TotalPages:  page.TotalPages,
		NextCommand: next,
	})
	return nil
}
