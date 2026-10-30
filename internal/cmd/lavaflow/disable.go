package lavaflow

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type disableOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newDisable(deps cliruntime.Deps) *cobra.Command {
	opts := disableOptions{}
	cmd := &cobra.Command{
		Use:   "disable",
		Short: "Turn LavaFlow off",
		Long: `Turn LavaFlow off for the current project. No new run starts; past runs and
their records stay readable. It needs a person's session: a project access
token is refused.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runDisable(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runDisable(ctx context.Context, opts disableOptions) error {
	settings, err := clilavaflow.NewService(opts.deps).Disable(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, settings)
	}
	output.LavaflowSettingsSaved(opts.out, "LavaFlow is off. Its runs and records stay readable.", settings)
	return nil
}
