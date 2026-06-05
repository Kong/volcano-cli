package functions

import (
	"github.com/spf13/cobra"

	schedulerscmd "github.com/Kong/volcano-cli/internal/cmd/functions/schedulers"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the functions command.
func New(deps cliruntime.Deps) *cobra.Command {
	return newWithOptions(deps, commandOptions{batchDeployAll: true})
}

// NewLocal returns the functions command for local-mode projects.
func NewLocal(deps cliruntime.Deps) *cobra.Command {
	return newWithOptions(deps, commandOptions{batchDeployAll: false})
}

type commandOptions struct {
	batchDeployAll bool
}

func newWithOptions(deps cliruntime.Deps, opts commandOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "functions",
		Short: "Manage functions",
		Long:  "List, inspect, update, delete, and view logs for cloud functions.",
	}
	cmd.AddCommand(newDeploy(deps, opts.batchDeployAll))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(newUpdate(deps))
	cmd.AddCommand(newLogs(deps))
	cmd.AddCommand(newRuntimes(deps))
	cmd.AddCommand(schedulerscmd.New(deps))
	return cmd
}
