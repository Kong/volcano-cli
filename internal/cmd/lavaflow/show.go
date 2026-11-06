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
		Long: `Show one LavaFlow run: the commit it read the code at, the diagnosis, root
cause and the evidence behind it, its review, and what happened to its fix.
The Fix block gives the pull request, its state and where its merge is, or
why the run opened none. A fix LavaFlow held or stopped lists why, and a
failed run says why, in the API's words. --json prints the run as the API
returned it.`,
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
	run, body, err := clilavaflow.NewService(opts.deps).GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeAPIJSON(opts.out, body)
	}
	output.LavaflowRun(opts.out, run)
	return nil
}
