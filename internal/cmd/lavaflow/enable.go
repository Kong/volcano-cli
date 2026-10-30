package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type toggleOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newEnable(deps cliruntime.Deps) *cobra.Command {
	opts := toggleOptions{}
	cmd := &cobra.Command{
		Use:   "enable",
		Short: "Turn LavaFlow on",
		Long: clilavaflow.OnDescription + `

This turns LavaFlow on for the current project. It needs a person's session: a
project access token is refused. Connect a repository with 'volcano git
connect' so LavaFlow can open fix pull requests. Choose how far it goes with
'` + cliruntime.CommandPath(deps, "lavaflow settings set --autonomy") + `'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runToggle(cmd.Context(), opts, true)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runToggle(ctx context.Context, opts toggleOptions, enabled bool) error {
	settings, err := clilavaflow.NewService(opts.deps).SetEnabled(ctx, enabled)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	message := "LavaFlow is off. Its runs and records stay readable."
	if enabled {
		message = "LavaFlow is on"
	}
	output.LavaflowSettingsSaved(opts.out, message, settings)
	return nil
}
