package lavaflow

import (
	"github.com/spf13/cobra"

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
	cmd.AddCommand(newRunsStart(deps), newRunsOpen(deps))
	return cmd
}
