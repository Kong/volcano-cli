package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type runsGetOptions struct {
	deps       cliruntime.Deps
	runID      string
	jsonOutput bool
	out        io.Writer
}

func newRunsGet(deps cliruntime.Deps) *cobra.Command {
	opts := runsGetOptions{}
	cmd := &cobra.Command{
		Use:   "get <run-id>",
		Short: "Show a LavaFlow run",
		Long: `Show one LavaFlow run: where it is, its diagnosis once it has one, and the
pull request it opened. --json adds the proposed fix's edits.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.runID = args[0]
			opts.out = cmd.OutOrStdout()
			return runRunsGet(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runRunsGet(ctx context.Context, opts runsGetOptions) error {
	run, err := clilavaflow.NewService(opts.deps).GetRun(ctx, opts.runID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, run)
	}
	output.LavaFlowRun(opts.out, run, output.LavaFlowCommands{
		Enable:     cliruntime.CommandPath(opts.deps, "lavaflow enable"),
		GitConnect: gitConnectCommand,
		RunsStart:  cliruntime.CommandPath(opts.deps, "lavaflow runs start"),
		RunsGet:    cliruntime.CommandPath(opts.deps, "lavaflow runs get"),
	})
	return nil
}
