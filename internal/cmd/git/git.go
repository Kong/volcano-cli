// Package git wires the volcano git subcommands.
package git

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/gitconnect"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the git command tree.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git",
		Short: "Manage the Git repository connected to the current project",
		Long: `Connect the current project to a GitHub repository so that pushes deploy.

Volcano never pushes for you and never stores push credentials: connecting only
binds the project to a repository you can already reach through your GitHub
connection. Pushing stays your own "git push", with the credentials already on
your machine.`,
	}
	cmd.AddCommand(newConnect(deps))
	cmd.AddCommand(newDisconnect(deps))
	return cmd
}

// guide rewrites the failures a user can act on into errors that say what to do
// next, and returns anything else unchanged. The command layer returns these
// like any other error: main prints them, so guidance belongs in the error
// value rather than in output written alongside it.
func guide(deps cliruntime.Deps, webURL string, err error) error {
	switch {
	case errors.Is(err, gitconnect.ErrProviderNotConfigured):
		return fmt.Errorf(
			"%w\n\nLocal mode does not ship the GitHub App settings, so Git connections are only "+
				"available against the cloud API", err)
	case errors.Is(err, gitconnect.ErrNoGitHubConnection):
		return fmt.Errorf("%w\n\n%s", err,
			dashboardStep(webURL, "Connect GitHub in the dashboard, then run this command again:"))
	case errors.Is(err, gitconnect.ErrNotConnected):
		return fmt.Errorf("%w\n\nConnect one with %s", err, cliruntime.CommandPath(deps, "git connect"))
	default:
		return err
	}
}

// dashboardStep points at the page that owns a flow the CLI cannot run itself.
// Connecting a GitHub account is a browser redirect bound to an HttpOnly
// cookie, so it cannot be completed from a terminal.
func dashboardStep(webURL, instruction string) string {
	if webURL == "" {
		return instruction
	}
	return fmt.Sprintf("%s\n  %s", instruction, webURL)
}
