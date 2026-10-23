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

const defaultRunsLimit = 20

type runsListOptions struct {
	deps       cliruntime.Deps
	limit      int
	cursor     string
	jsonOutput bool
	out        io.Writer
}

func newRunsList(deps cliruntime.Deps) *cobra.Command {
	opts := runsListOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List LavaFlow runs",
		Long:  "List the current project's LavaFlow runs, newest first.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runRunsList(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.limit, "limit", defaultRunsLimit,
		fmt.Sprintf("Number of runs per page (1-%d)", clilavaflow.MaxRunsLimit))
	cmd.Flags().StringVar(&opts.cursor, "cursor", "", "Continue from a returned next_cursor")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runRunsList(ctx context.Context, opts runsListOptions) error {
	page, err := clilavaflow.NewService(opts.deps).ListRuns(ctx, opts.limit, opts.cursor)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, page)
	}
	next := cliruntime.CommandPath(opts.deps, fmt.Sprintf("lavaflow runs list --limit %d", opts.limit))
	output.LavaFlowRuns(opts.out, page, next)
	return nil
}
