package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type showOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newShow(deps cliruntime.Deps) *cobra.Command {
	opts := showOptions{}
	cmd := &cobra.Command{
		Use:   "show <run-id>",
		Short: "Show one LavaFlow run",
		Long: `Show one LavaFlow run: the diagnosis, root cause and evidence, the log lines it
read, its review, the fix pull request, and the diff. A failed run says why in
the API's words. It needs the permission to read the project's logs.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runShow(cmd.Context(), opts, args[0])
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runShow(ctx context.Context, opts showOptions, runID string) error {
	run, err := clilavaflow.NewService(opts.deps).GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, run)
	}
	output.LavaflowRun(opts.out, run)
	return nil
}
