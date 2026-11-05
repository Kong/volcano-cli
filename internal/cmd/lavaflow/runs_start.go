package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newRun(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start LavaFlow runs",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newRunStart(deps))
	return cmd
}

type runStartOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newRunStart(deps cliruntime.Deps) *cobra.Command {
	opts := runStartOptions{}
	cmd := &cobra.Command{
		Use:   "start <issue-id>",
		Short: "Start a LavaFlow run on an issue",
		Long: `Start a LavaFlow run on one of the current project's issues. LavaFlow starts
runs on its own only for issues that hold an exception your code threw or
logged; start one here for any other issue, or for one recorded before
LavaFlow was turned on. The run follows the project's LavaFlow settings and
starts queued. Follow it with
'` + cliruntime.CommandPath(deps, "lavaflow show <run-id>") + `'.

Only the project's owner can start a run, while LavaFlow is on, from a live
sign-in: 'volcano login', or a platform token that expires. A project access
token is refused. An issue has one run in progress at a time, and starts
count toward LavaFlow's run limits: 5 runs per issue and 200 per project in
any 24 hours. --json prints the run as the API returned it.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runRunStart(cmd.Context(), opts, args[0])
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runRunStart(ctx context.Context, opts runStartOptions, issueID string) error {
	run, body, err := clilavaflow.NewService(opts.deps).StartRun(ctx, issueID)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeAPIJSON(opts.out, body)
	}
	output.LavaflowRunStarted(opts.out, run.Id.String(), run.Status,
		cliruntime.CommandPath(opts.deps, "lavaflow show "+run.Id.String()))
	return nil
}
