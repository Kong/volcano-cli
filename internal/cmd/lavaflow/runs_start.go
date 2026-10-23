package lavaflow

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

type runsStartOptions struct {
	deps       cliruntime.Deps
	issueID    string
	jsonOutput bool
	out        io.Writer
}

func newRunsStart(deps cliruntime.Deps) *cobra.Command {
	opts := runsStartOptions{}
	cmd := &cobra.Command{
		Use:   "start --issue <issue-id>",
		Short: "Diagnose an issue again",
		Long: fmt.Sprintf(`Queue a LavaFlow run for an issue, such as one whose last run failed. Take the
issue ID (trigger.issue.id) from an earlier run in '%s', or from the
Issues API (GET /projects/{id}/issues).

LavaFlow must be on. An issue has at most one queued or running run, and a
project starts at most 25 runs, on their own or by hand, in any 24 hours.
A run counts toward the project's usage.`, cliruntime.CommandPath(deps, "lavaflow runs list --json")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runRunsStart(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.issueID, "issue", "", "ID of the issue to diagnose")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	_ = cmd.MarkFlagRequired("issue")
	return cmd
}

func runRunsStart(ctx context.Context, opts runsStartOptions) error {
	run, err := clilavaflow.NewService(opts.deps).StartRun(ctx, opts.issueID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, run)
	}
	output.Success(opts.out, "LavaFlow run %s queued", run.Id)
	on := theme.On(opts.out)
	fmt.Fprintf(opts.out, "%s%s\n", theme.Dim("Follow it: ", on),
		theme.Command(cliruntime.CommandPath(opts.deps, "lavaflow runs get "+run.Id.String()), on))
	return nil
}
