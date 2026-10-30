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
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out of Volcano and end this login on the server",
		Long: fmt.Sprintf(`Log out: revoke the saved login's token and the session it came from on the
server, then remove the login, the current project, and function aliases from
~/.volcano/config.json. Other settings, such as telemetry, stay.

Only this login ends. Your other CLI logins and browser sessions stay signed
in: list them with '%[2]s', and end one with
'%[3]s'.

If the server can't be reached or fails, the command says so, exits non-zero,
and keeps the login saved, so running it again can end it, as can
'%[3]s'. If the server refuses to revoke the login, the
command removes it from this machine anyway and exits non-zero; the token then
stays valid until it expires. If it was a project access token, revoke it with
'%[1]s'. If it came from a sign-in, end that session with
'%[3]s' from another login.

A project access token (pt-) is removed from this machine but not revoked:
revoke it with '%[1]s'. A token in VOLCANO_TOKEN was never saved,
so logout doesn't unset it, and revokes it only when it is the saved token.`,
			cliruntime.CommandPath(deps, "cloud access-tokens revoke"),
			cliruntime.CommandPath(deps, "account sessions list"),
			cliruntime.CommandPath(deps, "account sessions revoke <id>"),
		),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd.Context(), deps, cmd.OutOrStdout())
		},
	}
}

func runLogout(ctx context.Context, deps cliruntime.Deps, w io.Writer) error {
	result := cliauth.NewService(deps).Logout(ctx)
	if result.Outcome == cliauth.LogoutNotRevoked {
		return fmt.Errorf("the server did not end this login: %w\n\n"+
			"The login is still saved on this machine. Run %s again, or end it with %s",
			result.RevokeErr, cliruntime.CommandPath(deps, "logout"), cliruntime.CommandPath(deps, "account sessions revoke <id>"))
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
	case cliauth.LogoutUnreadable:
		output.Success(w, "Logged out")
		output.Warning(w, "The saved login could not be read, so nothing was revoked on the server. "+
			"If it is still valid, it stays valid until it expires. %s", endElsewhere(deps))
	case cliauth.LogoutProjectToken:
		output.Success(w, "Logged out")
		output.Note(w, "The project access token stays valid: revoke it with %s.",
			cliruntime.CommandPath(deps, "cloud access-tokens revoke"))
	case cliauth.LogoutRefused:
		return fmt.Errorf("logged out of this machine, but the server did not end this login: %w\n\n%s",
			result.RevokeErr, refusedAdvice(deps, result.RevokeErr))
	}
	output.Note(w, "Removed the login from ~/.volcano/config.json.")
	switch {
	case result.EnvTokenEnded:
		output.Note(w, "%s holds the token this login used, which the server no longer accepts, "+
			"so commands that use it now fail. Unset it.", config.TokenEnv)
	case (&config.Config{}).TokenFromEnv():
		output.Note(w, "%s is still set, so commands keep using it. Logout neither revokes nor unsets it.", config.TokenEnv)
	}
	return nil
}

// endElsewhere says how to end a login this machine no longer holds. Only a
// token from a sign-in has a session to end; ending one issued without a
// session isn't available from the CLI yet.
func endElsewhere(deps cliruntime.Deps) string {
	return fmt.Sprintf("If it came from a sign-in, end that session with %s from another login. "+
		"Ending a token not tied to a session from the CLI isn't available yet.",
		cliruntime.CommandPath(deps, "account sessions revoke <id>"))
}

// refusedAdvice says what still holds after the server refused to end the
// login. A 403 means the login held a project access token, which only the
// access-token commands revoke and which is no account session. A 404 is a
// server without the route, where a session can still be ended another way.
func refusedAdvice(deps cliruntime.Deps, err error) string {
	switch api.Status(err) {
	case http.StatusForbidden:
		return fmt.Sprintf("This login held a project access token, which stays valid until it expires. "+
			"Revoke it with %s from a login that can manage the project",
			cliruntime.CommandPath(deps, "cloud access-tokens revoke <name-or-id>"))
	case http.StatusNotFound:
		return "The token stays valid until it expires. " + endElsewhere(deps)
	default:
		return "The token stays valid until it expires"
	}
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
