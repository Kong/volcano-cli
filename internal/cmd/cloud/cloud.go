// Package cloud wires commands that target the hosted Volcano API.
package cloud

import (
	"github.com/spf13/cobra"

	accesstokenscmd "github.com/Kong/volcano-cli/internal/cmd/accesstokens"
	configcmd "github.com/Kong/volcano-cli/internal/cmd/config"
	databasescmd "github.com/Kong/volcano-cli/internal/cmd/databases"
	deploycmd "github.com/Kong/volcano-cli/internal/cmd/deploy"
	deploymentscmd "github.com/Kong/volcano-cli/internal/cmd/deployments"
	domainscmd "github.com/Kong/volcano-cli/internal/cmd/domains"
	durablecmd "github.com/Kong/volcano-cli/internal/cmd/durable"
	frontendscmd "github.com/Kong/volcano-cli/internal/cmd/frontends"
	functionscmd "github.com/Kong/volcano-cli/internal/cmd/functions"
	lavaflowcmd "github.com/Kong/volcano-cli/internal/cmd/lavaflow"
	logscmd "github.com/Kong/volcano-cli/internal/cmd/logs"
	notificationscmd "github.com/Kong/volcano-cli/internal/cmd/notifications"
	sandboxescmd "github.com/Kong/volcano-cli/internal/cmd/sandboxes"
	storagecmd "github.com/Kong/volcano-cli/internal/cmd/storage"
	variablescmd "github.com/Kong/volcano-cli/internal/cmd/variables"
	"github.com/Kong/volcano-cli/internal/dataplane"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the cloud command tree.
func New(deps cliruntime.Deps) *cobra.Command {
	deps.CommandPathPrefix = "volcano cloud"
	cmd := &cobra.Command{
		Use:   "cloud",
		Short: "Manage cloud resources",
		Long:  "Manage resources in the current Volcano cloud project.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(NewResourceCommands(deps)...)
	return cmd
}

// NewResourceCommands returns cloud resource commands.
func NewResourceCommands(deps cliruntime.Deps) []*cobra.Command {
	deps.CommandPathPrefix = "volcano cloud"
	dataPlaneKeys := dataplane.NewService(deps)
	return []*cobra.Command{
		sandboxescmd.New(deps),
		accesstokenscmd.New(deps),
		configcmd.New(deps),
		databasescmd.New(deps),
		deploycmd.New(deps),
		deploymentscmd.New(deps),
		domainscmd.New(deps),
		durablecmd.New(deps),
		frontendscmd.New(deps),
		functionscmd.NewWithOptions(deps, functionscmd.WithInvokeTokenProvider(dataPlaneKeys.ServiceKeyForProject)),
		lavaflowcmd.New(deps),
		logscmd.New(deps),
		notificationscmd.New(deps),
		storagecmd.NewWithOptions(deps, storagecmd.WithObjectTokenProvider(dataPlaneKeys.ServiceKey)),
		variablescmd.New(deps),
	}
}
