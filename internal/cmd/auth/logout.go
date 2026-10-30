package auth

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

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

If the server can't revoke the login, the command says so, still removes it
from this machine, and exits non-zero. The token then stays valid until it
expires, so end it with '%[3]s' from another login,
or in the dashboard under Account, Sessions.

A project access token (pt-) is removed from this machine but not revoked:
revoke it with '%[1]s'. A token in VOLCANO_TOKEN was never saved,
so logout neither revokes nor unsets it.`,
			cliruntime.CommandPath(deps, "access-tokens revoke"),
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
	outcome, err := cliauth.NewService(deps).Logout(ctx)
	switch outcome {
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
			"If it is still valid, end it with %s from another login, or in the dashboard under Account, Sessions.",
			cliruntime.CommandPath(deps, "account sessions revoke <id>"))
	case cliauth.LogoutProjectToken:
		output.Success(w, "Logged out")
		output.Note(w, "The project access token stays valid: revoke it with %s.",
			cliruntime.CommandPath(deps, "access-tokens revoke"))
	case cliauth.LogoutNotRevoked:
		if err != nil {
			return fmt.Errorf("logged out of this machine, but the server did not revoke this login: %w\n\n"+
				"The token stays valid until it expires. End it with %s from another login, "+
				"or in the dashboard under Account, Sessions",
				err, cliruntime.CommandPath(deps, "account sessions revoke <id>"))
		}
	}
	if err != nil {
		return fmt.Errorf("failed to clear credentials: %w", err)
	}
	output.Note(w, "Removed the login from ~/.volcano/config.json.")
	if (&config.Config{}).TokenFromEnv() {
		output.Note(w, "%s is still set, so commands keep using it. Logout neither revokes nor unsets it.", config.TokenEnv)
	}
	return nil
}
