package frontends

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the frontends command.
func New(deps cliruntime.Deps) *cobra.Command {
	if deps.CommandPathPrefix == "" {
		deps.CommandPathPrefix = "volcano cloud"
	}
	cmd := &cobra.Command{
		Use:   "frontends",
		Short: "Manage frontends",
		Long:  "Deploy, list, inspect, redeploy, delete, view logs, and manage frontend custom domains.",
	}
	cmd.AddCommand(newDeploy(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(newRedeploy(deps))
	cmd.AddCommand(newLogs(deps))
	cmd.AddCommand(newDomain(deps))
	return cmd
}
