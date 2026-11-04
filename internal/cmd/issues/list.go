package issues

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliissues "github.com/Kong/volcano-cli/internal/issues"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps       cliruntime.Deps
	query      cliissues.ListQuery
	jsonOutput bool
	out        io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the project's issues",
		Long: `List the project's issues, most recently seen first: each one's status, the
function or frontend that raised it, its title, and how many errors it
counted. Show one with '` + cliruntime.CommandPath(deps, "issues get <issue-id>") + `'.`,
		Example: "  " + cliruntime.CommandPath(deps, "issues list --status open"),
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runList(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.query.Status, "status", "", "Only issues in this status: open, resolved or ignored")
	cmd.Flags().IntVar(&opts.query.Limit, "limit", cliissues.DefaultLimit, "Issues per page (1 to 100)")
	cmd.Flags().StringVar(&opts.query.Cursor, "cursor", "", "Start after this cursor, from the previous page")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	page, err := cliissues.NewService(opts.deps).List(ctx, opts.query)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return output.ControlSafeJSON(opts.out, page)
	}
	next := ""
	if page.HasMore && page.NextCursor != nil && *page.NextCursor != "" {
		next = cliruntime.CommandPath(opts.deps, fmt.Sprintf("issues list --limit %d --cursor %s", opts.query.Limit, *page.NextCursor))
		if opts.query.Status != "" {
			next += " --status " + opts.query.Status
		}
	}
	output.Issues(opts.out, output.IssuesPage{Issues: page.Data, NextCommand: next})
	return nil
}
