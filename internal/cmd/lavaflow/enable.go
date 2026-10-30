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

// errDeclined stops enable when the person answers no. The prompt has said
// so, and the command exits 0.
var errDeclined = errors.New("declined")

type enableOptions struct {
	deps       cliruntime.Deps
	yes        bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	errOut     io.Writer
}

func newEnable(deps cliruntime.Deps) *cobra.Command {
	opts := enableOptions{}
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Turn LavaFlow on",
		Long: clilavaflow.OnDescription + `

This turns LavaFlow on for the current project. Before it does, it says:
` + clilavaflow.EnableNotice + `
and asks you to confirm; --yes skips the question. It needs a person's
session: a project access token is refused. Connect a repository with 'volcano
git connect' so LavaFlow can open fix pull requests. Choose how far it goes
with '` + cliruntime.CommandPath(deps, "lavaflow settings set --autonomy") + `'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			return runEnable(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runEnable(ctx context.Context, opts enableOptions) error {
	settings, err := clilavaflow.NewService(opts.deps).Enable(ctx, func() error {
		return confirmEnable(opts)
	})
	if errors.Is(err, errDeclined) {
		return nil
	}
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	output.LavaflowSettingsSaved(opts.out, "LavaFlow is on", settings)
	return nil
}

// confirmEnable says where LavaFlow sends the project's data, even with --yes,
// and on stderr so --json output stays JSON. Without --yes it asks, and
// refuses when nothing can answer rather than read that as a no.
func confirmEnable(opts enableOptions) error {
	if opts.yes {
		fmt.Fprintln(opts.errOut, clilavaflow.EnableNotice)
		return nil
	}
	if !confirm.CanPrompt(opts.in) {
		return fmt.Errorf("%s\n\nstdin is not a terminal: confirmation required; pass --yes", clilavaflow.EnableNotice)
	}
	confirmed, err := confirm.Action(opts.in, opts.errOut, clilavaflow.EnableNotice, "Turn LavaFlow on?")
	if err != nil {
		return err
	}
	if !confirmed {
		return errDeclined
	}
	return nil
}
