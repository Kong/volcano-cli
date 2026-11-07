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
cause and the evidence behind it, its review, whether the repository's tests
ran on its fix, and what happened to the fix. A review that didn't pass the
fix lists the findings it left open and what it leaves to you to decide.
The Fix block gives the pull request, its state, where its merge is and when
the merged fix went live, or why the run opened none and what to do next. A
fix LavaFlow held or stopped says why, and a failed or cancelled run says why
in the API's words. Times are in UTC. The Checks line lists each command
the repository's checks ran and how it ended, with the output of one that
failed or timed out indented under it. --json prints the run as the API
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
	output.LavaflowRun(opts.out, run, output.LavaflowRunCommands{
		StartRun:    cliruntime.CommandPath(opts.deps, "lavaflow runs start "+run.IssueId.String()),
		SettingsSet: cliruntime.CommandPath(opts.deps, "lavaflow settings set"),
		List:        cliruntime.CommandPath(opts.deps, "lavaflow list"),
	})
	return nil
}
