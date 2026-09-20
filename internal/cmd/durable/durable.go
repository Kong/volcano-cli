// Package durable wires the volcano durable subcommands.
package durable

import (
	"github.com/spf13/cobra"

	executionscmd "github.com/Kong/volcano-cli/internal/cmd/durable/executions"
	schedulerscmd "github.com/Kong/volcano-cli/internal/cmd/durable/schedulers"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// NewLocal returns the durable functions command for the local tree.
//
// The same commands as the cloud tree, pointed at the local server. Local
// development runs durable executions on its own engine now, against the same
// API and the same manifest, so there is nothing for the two trees to differ
// about.
func NewLocal(deps cliruntime.Deps) *cobra.Command {
	return New(deps)
}

// New returns the durable functions command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "durable",
		Short: "Manage durable functions",
		Long: `Durable functions checkpoint their progress and resume where they left off,
so one execution can run far longer than a single invocation is allowed.

They are a separate collection from standard functions: a durable function is
started rather than invoked, its executions are tracked resources, and the two
collections never accept each other's names or ids.`,
	}
	cmd.AddCommand(newDeploy(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newStart(deps))
	cmd.AddCommand(newLogs(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(executionscmd.New(deps))
	cmd.AddCommand(schedulerscmd.New(deps))
	return cmd
}
