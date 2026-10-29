package git

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/gitconnect"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newAutoDeploy(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "auto-deploy on|off",
		Short: "Turn deploying on a push to the connected repository on or off",
		Long: fmt.Sprintf(`Turn deploying on a push to the connected repository's production branch on
or off. While it is off, a push deploys nothing, whatever else is selected.

Turning it off removes nothing: what is already deployed stays up. The other
deploy settings stay as they were; see '%s' for all of them.

This works with a project access token, so CI or an agent can set it. The
project needs a connected repository first: see '%s'.`,
			cliruntime.CommandPath(deps, "git status"),
			cliruntime.CommandPath(deps, "git connect")),
		Example:   "  volcano git auto-deploy on\n  volcano git auto-deploy off",
		Args:      cobra.ArbitraryArgs,
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			on, err := onOffArgument(args, "auto-deploy")
			if err != nil {
				return err
			}
			return runDeploySetting(cmd.Context(), deps, cmd.OutOrStdout(), deploySetting{
				subcommand: "auto-deploy",
				change: func(ctx context.Context, service gitconnect.Service) (*apiclient.ProjectGitDeploySettings, error) {
					return service.SetAutoDeploy(ctx, on)
				},
				render: output.GitAutoDeploySet,
			})
		},
	}
}
