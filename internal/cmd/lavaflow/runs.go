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

func newRuns(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Act on LavaFlow's runs",
		Long: `Act on LavaFlow's runs. List them with '` + cliruntime.CommandPath(deps, "lavaflow list") + `' and
show one with '` + cliruntime.CommandPath(deps, "lavaflow show <run-id>") + `'.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newRunsOpen(deps))
	return cmd
}

// openDisclosure is what opening a held fix does, said before the question.
const openDisclosure = "Opening the pull request lets every job above run this fix with the repository's secrets " +
	"before anyone reviews it on GitHub. Volcano opens it only while the fix and its jobs still match both fingerprints."

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

It first shows the fix's diff as LavaFlow's reviewers read it, every workflow
job that will get the repository's secrets, and two fingerprints: one for the
fix and one for the jobs. Then it asks, and the answer defaults to no. Volcano
opens the pull request only while the fix and its jobs still match both
fingerprints; if either changed, run this again.

A person decides, signed in with 'volcano login' or another platform token
that expires: a project access token is refused before anything is sent. At a
terminal it always asks. --yes answers the question only when stdin isn't a
terminal. What it shows and asks goes to stderr, so --json prints only the run
as the API returned it.`,
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
	run, body, err := clilavaflow.NewService(opts.deps).OpenHeldFix(ctx, runID, func(held *clilavaflow.HeldFix) error {
		output.LavaflowHeldFix(opts.errOut, held.Run, held.DiffSHA256, held.JobsSHA256)
		fmt.Fprintln(opts.errOut)
		return confirmOpen(opts)
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
	output.LavaflowHeldFixOpened(opts.out, run, cliruntime.CommandPath(opts.deps, "lavaflow show "+runID))
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
