package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type approveOptions struct {
	deps       cliruntime.Deps
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	prompt     io.Writer
}

func newApprove(deps cliruntime.Deps) *cobra.Command {
	opts := approveOptions{}
	cmd := &cobra.Command{
		Use:   "approve <run-id>",
		Short: "Approve the merge a LavaFlow run waits for",
		Long: `Approve the merge a LavaFlow run waits for. CI then runs on the fix, and
LavaFlow merges it once the required checks pass.

Before it asks, it shows the run, its pull request and why the merge waits for
a person. A run whose merge doesn't wait for a person is refused. To stop a
merge instead, close the pull request.

Only the project's owner approves, from their own session: a project access
token is refused. Use --yes to skip the prompt.`,
		Example: "  " + cliruntime.CommandPath(deps, "lavaflow approve 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			// The question stays off stdout under --json, so a script reads only
			// the run there.
			opts.prompt = opts.out
			if opts.jsonOutput {
				opts.prompt = cmd.ErrOrStderr()
			}
			return runApprove(cmd.Context(), opts, args[0])
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

// runApprove refuses a project access token first, as the API does. It then
// reads the run, both to show the person why its merge waits and to refuse
// without a write when it doesn't, and asks before approving.
func runApprove(ctx context.Context, opts approveOptions, runID string) error {
	service := clilavaflow.NewService(opts.deps)
	if err := service.RequirePerson(); err != nil {
		return err
	}
	run, err := service.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	if err := clilavaflow.AwaitsApproval(run); err != nil {
		return err
	}

	if !opts.yes {
		confirmed, err := confirm.Action(opts.in, opts.prompt, output.LavaflowApprovalContext(run), "Approve this merge?")
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	approved, err := service.Approve(ctx, run)
	if errors.Is(err, clilavaflow.ErrHeadMoved) {
		return fmt.Errorf("%w: check it again with '%s', then approve it",
			err, cliruntime.CommandPath(opts.deps, "lavaflow show "+run.Id.String()))
	}
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, approved)
	}
	output.Success(opts.out, "Approved: LavaFlow merges the fix once its required checks pass")
	output.LavaflowRun(opts.out, approved)
	return nil
}
