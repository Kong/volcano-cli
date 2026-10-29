// Package account wires the volcano account subcommands.
package account

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the account command group.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "account",
		Short: "Manage your Volcano account",
		Long:  "Manage your Volcano account: the sessions signed in to it, in browsers and the CLI.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newSessions(deps))
	return cmd
}
