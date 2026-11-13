package frontends

import (
	"fmt"
	"slices"

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
		Long:  "Deploy, list, inspect, redeploy, delete, view logs, and manage frontend custom domains, redirects, function and Frontend routes, and previews.",
	}
	cmd.AddCommand(newDeploy(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(newRedeploy(deps))
	cmd.AddCommand(newLogs(deps))
	cmd.AddCommand(newDomain(deps))
	cmd.AddCommand(newDomains(deps))
	cmd.AddCommand(newRoutes(deps))
	cmd.AddCommand(newPreview(deps))
	cmd.AddCommand(newServing(deps))
	cmd.AddCommand(newRollback(deps))
	return cmd
}

// NewLocal returns the frontends command for the local development server,
// which builds and serves frontends but has no custom domains.
func NewLocal(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "frontends",
		Short: "Manage frontends",
		Long:  "Deploy, list, inspect, redeploy, delete, view logs, and manage function and Frontend routes and previews of frontends on the local development server.",
	}
	cmd.AddCommand(newDeploy(deps))
	cmd.AddCommand(newList(deps))
	cmd.AddCommand(newGet(deps))
	cmd.AddCommand(newDelete(deps))
	cmd.AddCommand(newRedeploy(deps))
	cmd.AddCommand(newLogs(deps))
	cmd.AddCommand(newRoutes(deps))
	cmd.AddCommand(newPreview(deps))
	cmd.AddCommand(newServing(deps))
	cmd.AddCommand(newRollback(deps))
	cmd.AddCommand(newDomainCloudOnly("domain"))
	cmd.AddCommand(newDomainCloudOnly("domains"))
	return cmd
}

func newDomainCloudOnly(use string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: "Manage frontend custom domains",
		Long: fmt.Sprintf(`Manage frontend custom domains.

Local development serves no custom domains, so this is a cloud command: run
'volcano cloud frontends %s ...' instead.`, use),
		Hidden:             true,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Flag parsing is off, so cobra does not answer --help itself.
			if slices.ContainsFunc(args, func(arg string) bool { return arg == "--help" || arg == "-h" }) {
				return cmd.Help()
			}
			return fmt.Errorf("%q is a cloud command: local development serves no custom domains, "+
				"so run 'volcano cloud frontends %s ...' instead", "frontends "+use, use)
		},
	}
}
