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

func newFunctions(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "functions on|off",
		Short: "Choose whether a push to the connected repository deploys its functions",
		Long: fmt.Sprintf(`Choose whether a push to the connected repository's production branch deploys
the functions in the repository.

Turning it off removes nothing: functions already deployed stay up, and pushes
stop redeploying them. The other deploy settings stay as they were; see '%s'
for all of them.

This works with a project access token, so CI or an agent can set it. The
project needs a connected repository first: see '%s'.`,
			cliruntime.CommandPath(deps, "git status"),
			cliruntime.CommandPath(deps, "git connect")),
		Example:   "  volcano git functions on\n  volcano git functions off",
		Args:      cobra.ArbitraryArgs,
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			on, err := onOffArgument(args, "functions")
			if err != nil {
				return err
			}
			return runDeploySetting(cmd.Context(), deps, cmd.OutOrStdout(), deploySetting{
				subcommand: "functions",
				change: func(ctx context.Context, service gitconnect.Service) (*apiclient.ProjectGitDeploySettings, error) {
					return service.SetDeployFunctions(ctx, on)
				},
				render: output.GitFunctionsSet,
			})
		},
	}
}
