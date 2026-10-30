package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type settingsOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newSettings(deps cliruntime.Deps) *cobra.Command {
	opts := settingsOptions{}
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Show LavaFlow's settings",
		Long: `Show LavaFlow's settings for the current project: whether it is on, how far it
goes, and the custom context and system prompt in full. Change them with
'` + cliruntime.CommandPath(deps, "lavaflow settings set") + `'.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runSettings(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	cmd.AddCommand(newSettingsSet(deps))
	return cmd
}

func runSettings(ctx context.Context, opts settingsOptions) error {
	status, err := clilavaflow.NewService(opts.deps).Status(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, status.Settings)
	}
	output.LavaflowSettings(opts.out, status.Settings)
	return nil
}
