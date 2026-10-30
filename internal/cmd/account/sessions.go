package account

import (
	"fmt"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func newSessions(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List and sign out your account's sessions",
		Long: fmt.Sprintf(`List and sign out the sessions signed in to your account: the dashboard in each
browser, and each CLI login. These need an account login; a project access
token can't reach them.

To end the login this CLI uses, run '%s'.`, cliruntime.CommandPath(deps, "logout")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSessionsList(deps), newSessionsRevoke(deps))
	return cmd
}
