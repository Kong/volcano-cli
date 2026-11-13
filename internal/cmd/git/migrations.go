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

func newMigrations(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	baseline := cliruntime.CommandPath(deps, "databases migration baseline")
	cmd := &cobra.Command{
		Use:   "migrations on|off",
		Short: "Choose whether a push to the connected repository applies its database migrations",
		Long: fmt.Sprintf(`Choose whether a push to the connected repository's production branch applies
the commit's pending migrations, the SQL files under volcano/migrations, before
it deploys anything. Each database gets a tracked migration run; when one fails,
nothing from the push deploys and the live version keeps serving.

Anyone who can push to the production branch can then run any SQL as the
database owner, so protect that branch first. Turning it on asks for
confirmation; use --yes to skip the prompt.

Volcano applies only the files its migration history does not list yet. If
some files were already applied another way, record them first:
  %s

Turning it off undoes nothing: applied migrations stay, and pushes stop
applying new ones. The other deploy settings stay as they were; see
'%s' for all of them.

This works with a project access token, so CI or an agent can set it. The
project needs a connected repository first: see '%s'.`,
			baseline,
			cliruntime.CommandPath(deps, "git status"),
			cliruntime.CommandPath(deps, "git connect")),
		Example:   "  volcano git migrations on\n  volcano git migrations on --yes\n  volcano git migrations off",
		Args:      cobra.ArbitraryArgs,
		ValidArgs: []string{"on", "off"},
		RunE: func(cmd *cobra.Command, args []string) error {
			on, err := onOffArgument(args, "migrations")
			if err != nil {
				return err
			}
			setting := deploySetting{
				subcommand: "migrations",
				change: func(
					ctx context.Context, service gitconnect.Service, shown *apiclient.ProjectGitConnection,
				) (*apiclient.ProjectGitDeploySettings, error) {
					return service.SetApplyMigrations(ctx, on, shown)
				},
				render: output.GitMigrationsSet,
			}
			if on {
				setting.confirm = func(connection *apiclient.ProjectGitConnection) (bool, error) {
					return ask(cmd.InOrStdin(), cmd.OutOrStdout(), yes,
						fmt.Sprintf("Each push to %[1]s of %[2]s will apply the SQL files under volcano/migrations "+
							"to the project's databases as their owner, before it deploys. Anyone who can push "+
							"to %[1]s can then run any SQL on them, so protect that branch first.\n"+
							"Files already applied another way need a baseline first: %[3]s",
							connection.ProductionBranch, connection.RepoFullName, baseline),
						"Apply migrations on push?")
				}
			}
			return runDeploySetting(cmd.Context(), deps, cmd.OutOrStdout(), setting)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt when turning migrations on")
	return cmd
}
