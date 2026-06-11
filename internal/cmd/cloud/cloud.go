// Package cloud wires commands that target the hosted Volcano API.
package cloud

import (
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/cmd/cmdutil"
	configcmd "github.com/Kong/volcano-cli/internal/cmd/config"
	databasescmd "github.com/Kong/volcano-cli/internal/cmd/databases"
	frontendscmd "github.com/Kong/volcano-cli/internal/cmd/frontends"
	functionscmd "github.com/Kong/volcano-cli/internal/cmd/functions"
	storagecmd "github.com/Kong/volcano-cli/internal/cmd/storage"
	variablescmd "github.com/Kong/volcano-cli/internal/cmd/variables"
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
	return []*cobra.Command{
		configcmd.New(deps),
		databasescmd.New(deps),
		frontendscmd.New(deps),
		functionscmd.New(deps),
		storagecmd.New(deps),
		variablescmd.New(deps),
	}
}

// NewDeprecatedFrontendAlias returns the legacy direct frontend cloud command.
func NewDeprecatedFrontendAlias(deps cliruntime.Deps) *cobra.Command {
	deps.CommandPathPrefix = "volcano cloud"
	cmd := frontendscmd.New(deps)
	return cmdutil.HideDeprecatedAlias(cmd, `warning: "volcano frontends ..." is deprecated; use "volcano cloud frontends ..."`)
}
