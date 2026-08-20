// Package git wires the volcano git subcommands.
package git

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"
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

Volcano never pushes for you and never stores push credentials: connecting only
binds the project to a repository you can already reach through your GitHub
connection. Pushing stays your own "git push", with the credentials already on
your machine.`,
	}
	cmd.AddCommand(newStatus(deps))
	cmd.AddCommand(newConnect(deps))
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
	if !canPrompt(in) {
		return false, fmt.Errorf("%s\n\nThis needs confirmation and stdin is not a terminal. Pass --yes to proceed", warning)
	}
	return confirm.Action(in, out, warning, question)
}

// canPrompt reports whether there is a human on the other end of in. A reader
// that is not the process's stdin at all — an injected one, as in tests — is
// treated as promptable, since something is deliberately feeding it answers.
func canPrompt(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return true
	}
	return term.IsTerminal(f.Fd())
}

// guide rewrites the failures a user can act on into errors that say what to do
// next, and returns anything else unchanged. The command layer returns these
// like any other error: main prints them, so guidance belongs in the error
// value rather than in output written alongside it.
func guide(deps cliruntime.Deps, webURL string, err error) error {
	switch {
	case errors.Is(err, gitconnect.ErrProviderNotConfigured):
		return fmt.Errorf(
			"%w\n\nIf this is local mode, that is expected: the local stack ships without GitHub "+
				"App settings. Run against the cloud API", err)
	case errors.Is(err, gitconnect.ErrNoGitHubConnection):
		return fmt.Errorf("%w\n\n%s", err,
			dashboardStep(webURL, "Connect GitHub in the dashboard, then run this command again:"))
	case errors.Is(err, gitconnect.ErrNotAuthenticated):
		// The session first, because that is the cause the contract documents for
		// this 401. The dashboard is kept as the fallback only because a revoked
		// GitHub connection is not documented on these routes at all, so it
		// cannot be ruled out — not because anything says it is likelier.
		return fmt.Errorf("%w\n\nSign in again, then run this command again:\n  %s\n\n%s",
			err,
			cliruntime.CommandPath(deps, "login"),
			dashboardStep(webURL, "If you are already signed in, reconnect GitHub in the dashboard:"))
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

// dashboardStep points at the page that owns a flow the CLI cannot run itself.
// Connecting a GitHub account is a browser redirect bound to an HttpOnly
// cookie, so it cannot be completed from a terminal.
func dashboardStep(webURL, instruction string) string {
	if webURL == "" {
		return instruction
	}
	return fmt.Sprintf("%s\n  %s", instruction, webURL)
}
