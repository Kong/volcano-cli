// Package git wires the volcano git subcommands.
package git

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/gitconnect"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// New returns the git command tree.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "git",
		Short: "Manage the Git repository connected to the current project",
		Long: `Connect the current project to a GitHub repository so that pushes deploy.

Volcano never stores push credentials: connecting only binds the project to a
repository you can already reach through your GitHub connection. Pushing stays
your own "git push", with the credentials already on your machine. The one
exception is "volcano git export", which writes the project's stored source to
an empty repository once; Volcano pushes that first commit itself.`,
	}
	cmd.AddCommand(newStatus(deps))
	cmd.AddCommand(newRepos(deps))
	cmd.AddCommand(newConnect(deps))
	cmd.AddCommand(newRootDirectory(deps))
	cmd.AddCommand(newProductionBranch(deps))
	cmd.AddCommand(newAutoDeploy(deps))
	cmd.AddCommand(newFunctions(deps))
	cmd.AddCommand(newFrontend(deps))
	cmd.AddCommand(newExport(deps))
	cmd.AddCommand(newDisconnect(deps))
	return cmd
}

// ask puts a confirmation to the user, unless there is nobody to ask.
//
// A prompt read from a closed or piped stdin comes back as a decline, which
// would exit 0 having done nothing — the worst outcome for the agents and CI
// jobs these commands are meant to serve, and indistinguishable from success.
// Refusing outright names the fix instead. A human who answers "no" still
// cancels quietly, exit 0, as everywhere else in the CLI.
func ask(in io.Reader, out io.Writer, yes bool, warning, question string) (bool, error) {
	if yes {
		return true, nil
	}
	if !confirm.CanPrompt(in) {
		return false, fmt.Errorf("%s\n\nThis needs confirmation and stdin is not a terminal. Pass --yes to proceed", warning)
	}
	return confirm.Action(in, out, warning, question)
}

// guide rewrites the failures a user can act on into errors that say what to do
// next, and returns anything else unchanged. The command layer returns these
// like any other error: main prints them, so guidance belongs in the error
// value rather than in output written alongside it.
func guide(deps cliruntime.Deps, err error) error {
	switch {
	case errors.Is(err, gitconnect.ErrProviderNotConfigured):
		return fmt.Errorf(
			"%w\n\nIf this is local mode, that is expected: the local stack ships without GitHub "+
				"App settings. Run against the cloud API", err)
	case errors.Is(err, gitconnect.ErrSourceExportUnavailable):
		return fmt.Errorf(
			"%w\n\nIf this is local mode, that is expected: local mode serves functions from a directory "+
				"on your machine, so Volcano stores no source to export. Run against the cloud API", err)
	case errors.Is(err, gitconnect.ErrNoGitHubConnection):
		return fmt.Errorf("%w\n\n%s", err, cannotConnectGitHub)
	case errors.Is(err, gitconnect.ErrNotAuthenticated):
		// The session first, because that is the cause the contract documents for
		// this 401. A revoked GitHub connection is not documented on these routes
		// at all, so it cannot be ruled out, and comes second.
		return fmt.Errorf("%w\n\nSign in again, then run this command again:\n  %s\n\n"+
			"If you are already signed in, your GitHub connection may have been revoked. %s",
			err, cliruntime.CommandPath(deps, "login"), cannotConnectGitHub)
	case errors.Is(err, gitconnect.ErrBindingChanged):
		return fmt.Errorf("%w\n\nNothing was changed. Run %s to see where it stands",
			err, cliruntime.CommandPath(deps, "git status"))
	case errors.Is(err, gitconnect.ErrProjectNotFound):
		return fmt.Errorf("%w\n\nSelect one with %s, or check VOLCANO_PROJECT_ID",
			err, cliruntime.CommandPath(deps, "use <project>"))
	case errors.Is(err, gitconnect.ErrNotConnected):
		// No hedging about the project: the ambiguous 404 behind this is
		// resolved before it gets here.
		return fmt.Errorf("%w\n\nConnect one with %s", err, cliruntime.CommandPath(deps, "git connect"))
	default:
		return err
	}
}

// Connecting a GitHub account is a browser redirect bound to an HttpOnly
// cookie, which no API route starts yet. Name what is missing rather than send
// anyone to a web page; the CLI gains the step when the API does.
const cannotConnectGitHub = "The CLI cannot connect a GitHub account yet."
