package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type statsOptions struct {
	deps       cliruntime.Deps
	days       int
	jsonOutput bool
	out        io.Writer
}

func newStats(deps cliruntime.Deps) *cobra.Command {
	opts := statsOptions{}
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show the issues and fix pull requests LavaFlow opened per day",
		Long: `Show how many issues LavaFlow took on and how many fix pull requests it opened
in the current project, per UTC day, today included.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runStats(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.days, "days", clilavaflow.DefaultDays, "Days to show, today included (1 to 90)")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runStats(ctx context.Context, opts statsOptions) error {
	stats, err := clilavaflow.NewService(opts.deps).Stats(ctx, opts.days)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, stats)
	}
	output.LavaflowStats(opts.out, stats)
	return nil
}
