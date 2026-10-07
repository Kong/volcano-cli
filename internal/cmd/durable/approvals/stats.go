package approvals

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	clidurable "github.com/Kong/volcano-cli/internal/durable"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type statsOptions struct {
	deps       cliruntime.Deps
	function   string
	since      string
	jsonOutput bool
	out        io.Writer
}

func newStats(deps cliruntime.Deps) *cobra.Command {
	opts := statsOptions{}
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Count durable approvals by outcome",
		Long: `Count the project's approvals by outcome over a window: how many were
approved, denied, expired, or are still pending, the approval rate, the median
time to a decision, and the workflows that asked most.

An approval is counted in the window it was requested in. The window can reach
back up to a year.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "durable approvals stats"),
			cliruntime.CommandPath(deps, "durable approvals stats --function order-pipeline --since 7d")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runStats(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.function, "function", "", "Only approvals requested by this durable function (name or id)")
	cmd.Flags().StringVar(&opts.since, "since", "30d", "Window to count, such as 30d or 24h, up to 366d")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

// maxStatsWindow is the longest window the API counts.
const maxStatsWindow = 366 * 24 * time.Hour

func runStats(ctx context.Context, opts statsOptions) error {
	from, to, err := sinceWindow(opts.since)
	if err != nil {
		return err
	}
	if to.Sub(from) > maxStatsWindow {
		return fmt.Errorf("invalid --since %q: stats reach back at most 366 days", opts.since)
	}

	stats, err := clidurable.NewService(opts.deps).ApprovalStats(ctx, strings.TrimSpace(opts.function), &from, &to)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, stats)
	}
	output.DurableApprovalStats(opts.out, stats)
	return nil
}
