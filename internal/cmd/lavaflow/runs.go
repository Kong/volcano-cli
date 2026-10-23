package lavaflow

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newRuns(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runs",
		Short: "Inspect LavaFlow runs",
		Long: `Inspect LavaFlow runs. A run diagnoses one issue and, when a repository is
connected and a code change would fix it, opens a draft pull request with the
fix. Runs stay readable after LavaFlow is turned off.`,
	}
	cmd.AddCommand(newRunsList(deps))
	cmd.AddCommand(newRunsGet(deps))
	cmd.AddCommand(newRunsStart(deps))
	return cmd
}
