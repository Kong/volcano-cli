package lavaflow

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newDisable(deps cliruntime.Deps) *cobra.Command {
	opts := toggleOptions{}
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
			return runToggle(cmd.Context(), opts, false)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}
