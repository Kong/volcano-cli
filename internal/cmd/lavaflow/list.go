package lavaflow

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps       cliruntime.Deps
	query      clilavaflow.RunsQuery
	jsonOutput bool
	out        io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List LavaFlow's runs",
		Long: `List LavaFlow's runs in the current project, newest first: what each worked on,
where it got to, and the fix pull request it opened. Show one with
'` + cliruntime.CommandPath(deps, "lavaflow show <run-id>") + `'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runList(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.query.Limit, "limit", clilavaflow.DefaultLimit, "Runs per page (1 to 100)")
	cmd.Flags().StringVar(&opts.query.Cursor, "cursor", "", "Start after this cursor, from the previous page")
	cmd.Flags().StringVar(&opts.query.Status, "status", "", "Only runs with this status, such as failed or pr_open")
	cmd.Flags().StringVar(&opts.query.ResourceID, "resource", "", "Only runs for this resource ID, from 'lavaflow resources --json'")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	page, err := clilavaflow.NewService(opts.deps).ListRuns(ctx, opts.query)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, page)
	}
	next := ""
	if page.HasMore && page.NextCursor != nil && *page.NextCursor != "" {
		next = cliruntime.CommandPath(opts.deps, fmt.Sprintf("lavaflow list --limit %d --cursor %s", opts.query.Limit, *page.NextCursor))
		if opts.query.Status != "" {
			next += " --status " + opts.query.Status
		}
		if opts.query.ResourceID != "" {
			next += " --resource " + opts.query.ResourceID
		}
	}
	output.LavaflowRuns(opts.out, output.LavaflowRunsPage{Runs: page.Data, NextCommand: next})
	return nil
}
