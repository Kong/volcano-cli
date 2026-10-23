package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type enableOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newEnable(deps cliruntime.Deps) *cobra.Command {
	opts := enableOptions{}
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Turn LavaFlow on",
		Long: `Turn LavaFlow on for the current project. ` + clilavaflow.OnDescription + `

Runs start without a request, and each one counts toward the project's usage.
` + clilavaflow.CostNote + `

Turning LavaFlow on needs a paid plan and a person's login: a project access
token is refused. Connect a repository with 'volcano git connect' so LavaFlow
can open fix pull requests; without one it diagnoses issues only.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runEnable(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runEnable(ctx context.Context, opts enableOptions) error {
	result, err := clilavaflow.NewService(opts.deps).SetEnabled(ctx, true)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, result.Settings)
	}
	output.LavaFlowEnabled(opts.out, result.Settings, clilavaflow.OnDescription, clilavaflow.CostNote)
	return nil
}
