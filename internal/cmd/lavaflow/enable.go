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

This turns LavaFlow on for the current project. Before it does, it prints
the sentence above and:
` + clilavaflow.EnableNotice + `
and asks you to confirm; --yes skips the question. Only the project's owner
can turn it on, from their own session: a project access token is refused. The
owner's plan has to include LavaFlow. Connect a repository with 'volcano
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

// confirmEnable says what LavaFlow does and where it sends the project's
// data before it asks.
func confirmEnable(opts enableOptions) error {
	return confirmChange(opts.yes, opts.in, opts.errOut, clilavaflow.EnableDisclosure, "Turn LavaFlow on?")
}

// confirmChange says what a change does, even with --yes, on stderr so --json
// output stays JSON. Without --yes it asks, and refuses when nothing can
// answer rather than read that as a no.
func confirmChange(yes bool, in io.Reader, errOut io.Writer, disclosure, question string) error {
	if yes {
		fmt.Fprintln(errOut, disclosure)
		return nil
	}
	if !confirm.CanPrompt(in) {
		return fmt.Errorf("%s\n\nstdin is not a terminal: confirmation required; pass --yes", disclosure)
	}
	confirmed, err := confirm.Action(in, errOut, disclosure, question)
	if err != nil {
		return err
	}
	if !confirmed {
		return errDeclined
	}
	return nil
}
