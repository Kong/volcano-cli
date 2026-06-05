package schedulers

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the schedulers command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedulers",
		Short: "Manage scheduled function invocations",
		Long: `Function schedulers invoke a function automatically on a cron schedule.

By default Volcano chooses one deployed region for the scheduler. You may
optionally pin the scheduler to one deployed region with --regions.`,
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newEnable(deps))
	cmd.AddCommand(newDisable(deps))
	cmd.AddCommand(newDelete(deps))
	return cmd
}
