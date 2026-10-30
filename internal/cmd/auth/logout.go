package auth

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	cliauth "github.com/Kong/volcano-cli/internal/auth"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// NewLogout returns the logout command.
func NewLogout(deps cliruntime.Deps) *cobra.Command {
	var noRevoke bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out of Volcano and end this login on the server",
		Long: fmt.Sprintf(`Log out: revoke the saved login's token and the session it came from on the
server, then remove the login, the current project, and function aliases from
~/.volcano/config.json. Other settings, such as telemetry, stay.

Only this login ends. Your other CLI logins and browser sessions stay signed
in.

If the server can't be reached or fails, the command says so, exits non-zero,
and keeps the login saved, so running it again can end it. If the server
refuses to revoke the login, the command removes it from this machine anyway
and exits non-zero; the token then stays valid until it expires. If it was a
project access token, revoke it with '%[1]s'.

--no-revoke skips the server: it only removes the login from this machine, and
the token stays valid until it expires. Use it when the server can't be
reached.

A project access token (pt-) is removed from this machine but not revoked:
revoke it with '%[1]s'. A token in VOLCANO_TOKEN was never saved,
so logout doesn't unset it, and revokes it only when it is the saved token.`,
			cliruntime.CommandPath(deps, "cloud access-tokens revoke"),
		),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd.Context(), deps, cmd.OutOrStdout(), noRevoke)
		},
	}
	cmd.Flags().BoolVar(&noRevoke, "no-revoke", false, "Remove the login from this machine only, without ending it on the server")
	return cmd
}

func runLogout(ctx context.Context, deps cliruntime.Deps, w io.Writer, noRevoke bool) error {
	service := cliauth.NewService(deps)
	var result cliauth.LogoutResult
	if noRevoke {
		result = service.LogoutWithoutRevoke()
	} else {
		result = service.Logout(ctx)
	}
	if result.Outcome == cliauth.LogoutNotRevoked {
		return fmt.Errorf("the server did not end this login: %w\n\n"+
			"The login is still saved on this machine. Run %s again, or %s to remove it from this machine only",
			result.RevokeErr, cliruntime.CommandPath(deps, "logout"), cliruntime.CommandPath(deps, "logout --no-revoke"))
	}
	if result.ClearErr != nil {
		return clearFailed(result)
	}
	switch result.Outcome {
	case cliauth.LogoutRevoked:
		output.Success(w, "Logged out: this login's token and session were revoked on the server")
	case cliauth.LogoutAlreadyEnded:
		output.Success(w, "Logged out: the server had already ended this login, so there was nothing to revoke")
	case cliauth.LogoutNothingSaved:
		output.Success(w, "Logged out: no login was saved, so nothing was revoked")
	case cliauth.LogoutNoServerSession:
		output.Success(w, "Logged out: this server keeps no sign-in session, so there was nothing to revoke")
	case cliauth.LogoutNotAsked:
		output.Success(w, "Logged out of this machine")
		output.Warning(w, "The login was not ended on the server, so its token stays valid until it expires.")
	case cliauth.LogoutUnreadable:
		output.Success(w, "Logged out")
		output.Warning(w, "The saved login could not be read, so nothing was revoked on the server.")
	case cliauth.LogoutProjectToken:
		output.Success(w, "Logged out")
		output.Note(w, "The project access token stays valid: revoke it with %s.",
			cliruntime.CommandPath(deps, "cloud access-tokens revoke"))
	case cliauth.LogoutRefused:
		return fmt.Errorf("logged out of this machine, but the server did not end this login: %w\n\n%s",
			result.RevokeErr, refusedAdvice(deps, result))
	}
	switch {
	case result.Replaced:
		output.Note(w, "Another login was saved on this machine meanwhile, so it was kept.")
	case result.Outcome != cliauth.LogoutNothingSaved:
		output.Note(w, "Removed the login from ~/.volcano/config.json.")
	}
	switch {
	case result.EnvTokenEnded:
		output.Note(w, "%s holds the token this login used, which the server no longer accepts, "+
			"so commands that use it now fail. Unset it.", config.TokenEnv)
	case (&config.Config{}).TokenFromEnv():
		output.Note(w, "%s is still set, so commands keep using it. Logout neither revokes nor unsets it.", config.TokenEnv)
	}
	return nil
}

// refusedAdvice says what still holds after the server refused to end the
// login. The route refuses a project access token with 403, which only the
// access-token commands revoke; a token with the account prefix can't be one.
func refusedAdvice(deps cliruntime.Deps, result cliauth.LogoutResult) string {
	if api.Status(result.RevokeErr) == http.StatusForbidden && !result.AccountToken {
		return fmt.Sprintf("This login held a project access token, which stays valid until it expires. "+
			"Revoke it with %s from a login that can manage the project",
			cliruntime.CommandPath(deps, "cloud access-tokens revoke <name-or-id>"))
	}
	return "The token stays valid until it expires"
}

// clearFailed explains a login the server answered for but this machine could
// not remove, saying first what the server did.
func clearFailed(result cliauth.LogoutResult) error {
	switch result.Outcome {
	case cliauth.LogoutRevoked, cliauth.LogoutAlreadyEnded:
		return fmt.Errorf("the server ended this login, but removing it from this machine failed: %w", result.ClearErr)
	case cliauth.LogoutRefused:
		return fmt.Errorf("the server did not end this login (%w), and removing it from this machine failed: %w",
			result.RevokeErr, result.ClearErr)
	default:
		return fmt.Errorf("failed to remove the login from this machine: %w", result.ClearErr)
	}
}
