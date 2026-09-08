// Package schedulers wires the volcano durable schedulers subcommands.
package schedulers

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the durable schedulers command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schedulers",
		Short: "Manage scheduled durable executions",
		Long: `A durable scheduler starts an execution on a cron schedule instead of invoking
the function, so each tick produces an execution you can list, follow, and stop
like any other.

A tick draws on the same invocation allowance and concurrency cap a manual start
does, so a tick that would exceed the cap fails that run rather than queueing.`,
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newCreate(deps))
	cmd.AddCommand(newEnable(deps))
	cmd.AddCommand(newDisable(deps))
	cmd.AddCommand(newDelete(deps))
	return cmd
}
