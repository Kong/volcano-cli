// Package executions wires the volcano durable executions subcommands.
package executions

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the durable executions command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "executions",
		Short: "Inspect durable executions",
		Long: `An execution is one run of a durable function, and a resource of its own:
it has a name that makes a repeated start idempotent, a region it is pinned to,
and a result that is kept for as long as the function's retention allows.`,
	}
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newStop(deps))
	return cmd
}
