package lavaflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// openDisclosure is what opening a held fix does, said before the question.
const openDisclosure = "Opening the pull request lets every job above run this fix with the repository's secrets " +
	"before anyone reviews it on GitHub. Volcano opens it only while the diff and its jobs still match both fingerprints."

type runsOpenOptions struct {
	deps cliruntime.Deps
	// yes is --yes given explicitly. It answers only when stdin isn't a
	// terminal: a person at a terminal always answers the question.
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
}

func newRunsOpen(deps cliruntime.Deps) *cobra.Command {
	opts := runsOpenOptions{}
	cmd := &cobra.Command{
		Use:   "open <run-id>",
		Short: "Open the pull request for a fix LavaFlow held",
		Long: `Open the pull request for a fix LavaFlow held because a workflow in the
repository could run it with the repository's secrets before a person
approves it. Opening it lets those workflow jobs run it.

It first shows the fix's diff exactly as Volcano serves it, every workflow job
that will get the repository's secrets, and two fingerprints: one for the diff
and one for the jobs. Then it asks, and the answer defaults to no. Volcano
opens the pull request only while the diff and its jobs still match both
fingerprints. If either changed, it shows them again and asks again, up to
` + strconv.Itoa(clilavaflow.MaxOpenRounds) + ` times.

A diff that leaves out a file because it may contain a credential, or that
is cut short, isn't fully reviewable: it's shown with a warning, and LavaFlow
can't open it.

Only the project's owner can open one, while LavaFlow is on, from a live
sign-in: 'volcano login', or a platform token that expires. Their GitHub
account must be able to push to the project's repository. A project access
token is refused before anything is sent. At a terminal it always asks.
--yes answers the question only when stdin isn't a terminal, and then a
changed fix ends the command. What it shows and asks goes to stderr, so
--json prints only the queued run as the API returned it.`,
		Example: "  " + cliruntime.CommandPath(deps, "lavaflow runs open 9c1e4f2a-7b3d-4e8f-a1c2-5d6e7f8a9b0c"),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.yes = opts.yes && cmd.Flags().Changed("yes")
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			return runRunsOpen(cmd.Context(), opts, args[0])
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Open without the question when stdin isn't a terminal")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runRunsOpen(ctx context.Context, opts runsOpenOptions, runID string) error {
	run, body, err := clilavaflow.NewService(opts.deps).OpenHeldFix(ctx, runID, clilavaflow.OpenPrompt{
		Show: func(held *clilavaflow.HeldFix, round int) {
			if round > 1 {
				output.Warning(opts.errOut, "The fix or its jobs changed since you looked. Read them again:")
				fmt.Fprintln(opts.errOut)
			}
			output.LavaflowHeldFix(opts.errOut, held)
			fmt.Fprintln(opts.errOut)
		},
		Confirm: func(*clilavaflow.HeldFix) error { return confirmOpen(opts) },
		// Only a person at a terminal reads a changed fix again; --yes
		// answered for the fix first shown, not a changed one.
		Reask: confirm.CanPrompt(opts.in),
	})
	if errors.Is(err, errDeclined) {
		return nil
	}
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeAPIJSON(opts.out, body)
	}
	output.LavaflowHeldFixQueued(opts.out, run.Id.String(), cliruntime.CommandPath(opts.deps, "lavaflow show "+run.Id.String()))
	return nil
}

// confirmOpen asks at a terminal, even with --yes, and refuses when nothing
// can answer unless --yes was given. The answer defaults to no.
func confirmOpen(opts runsOpenOptions) error {
	if !confirm.CanPrompt(opts.in) {
		if !opts.yes {
			return errors.New("stdin is not a terminal: nothing was opened; pass --yes to open the fix without the question")
		}
		fmt.Fprintln(opts.errOut, openDisclosure)
		return nil
	}
	confirmed, err := confirm.Action(opts.in, opts.errOut, openDisclosure, "Open the fix's pull request?")
	if err != nil {
		return err
	}
	if !confirmed {
		return errDeclined
	}
	return nil
}
