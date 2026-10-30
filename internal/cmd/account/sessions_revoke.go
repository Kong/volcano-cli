package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	cliaccount "github.com/Kong/volcano-cli/internal/account"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// allOthersWarning says what signing out every other session reaches, which
// the name alone undersells: the dashboard in every browser goes with it.
const allOthersWarning = "This signs out every other session on your account: the dashboard in every browser, " +
	"and every other CLI login. This CLI stays logged in."

// everySessionWarning is the same for a login with no session of its own, as
// one with a token issued without a sign-in: there is no session to keep, so
// every one ends.
const everySessionWarning = "This CLI's login has no session of its own (its token wasn't issued by a sign-in), so this signs " +
	"out every session on your account: the dashboard in every browser, and every CLI login. This CLI stays logged in."

type sessionsRevokeOptions struct {
	deps      cliruntime.Deps
	sessionID uuid.UUID
	allOthers bool
	yes       bool
	in        io.Reader
	out       io.Writer
}

func newSessionsRevoke(deps cliruntime.Deps) *cobra.Command {
	opts := sessionsRevokeOptions{deps: deps}
	cmd := &cobra.Command{
		Use:   "revoke <session-id> | --all-others",
		Short: "Sign out one session, or every other one",
		Long: fmt.Sprintf(`Sign out one of your account's sessions, named by an ID from
'%[1]s'. A CLI login made from that session stops
working at once.

--all-others signs out every session except this CLI's own, browser sessions
included, so the dashboard asks every browser to sign in again. A login whose
token wasn't issued by a sign-in, such as one created in the dashboard, has no
session of its own, so from it --all-others signs out every session; the
command says which before it asks. --yes skips the question. Tokens not tied
to a session keep working: revoke the one this CLI uses with '%[2]s',
unless it comes from VOLCANO_TOKEN, which logout doesn't revoke.
Revoking your other platform tokens from the CLI is not available yet.

This CLI's own session ends with '%[2]s', which also removes the
login from this machine.`,
			cliruntime.CommandPath(deps, "account sessions list"),
			cliruntime.CommandPath(deps, "logout")),
		Example: "  volcano account sessions revoke 0b8e1c4e-5f0a-4d43-9a43-6f1f3d2b7c11\n" +
			"  volcano account sessions revoke --all-others",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := revokeArguments(args, opts.allOthers)
			if err != nil {
				return err
			}
			opts.sessionID = sessionID
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			return runSessionsRevoke(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.allOthers, "all-others", false,
		"Sign out every session except this CLI's own, browser sessions included")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip the confirmation prompt for --all-others")
	return cmd
}

// revokeArguments is the session to sign out: one named by ID, or none with
// --all-others.
func revokeArguments(args []string, allOthers bool) (uuid.UUID, error) {
	switch {
	case allOthers && len(args) == 1:
		return uuid.Nil, errors.New("pass a session ID or --all-others, not both")
	case allOthers:
		return uuid.Nil, nil
	case len(args) == 0:
		return uuid.Nil, errors.New("name the session to sign out, such as one from 'volcano account sessions list', " +
			"or pass --all-others")
	}
	raw := strings.TrimSpace(args[0])
	sessionID, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("%q is not a session ID: use an ID from 'volcano account sessions list'", raw)
	}
	return sessionID, nil
}

func runSessionsRevoke(ctx context.Context, opts sessionsRevokeOptions) error {
	service := cliaccount.NewService(opts.deps)
	if !opts.allOthers {
		if err := service.Revoke(ctx, opts.sessionID); err != nil {
			if errors.Is(err, cliaccount.ErrCurrentSession) {
				if (&config.Config{}).TokenFromEnv() {
					// logout ends the saved login, not the token in the
					// environment, so it would end the wrong one.
					return fmt.Errorf("%w, through %s. To end it, unset %s and run this command again with another login",
						err, config.TokenEnv, config.TokenEnv)
				}
				return fmt.Errorf("%w. To end it, run %s, which also removes the login from this machine",
					err, cliruntime.CommandPath(opts.deps, "logout"))
			}
			return err
		}
		output.Success(opts.out, "Signed out session %s", opts.sessionID)
		return nil
	}

	scope, err := service.OthersScope(ctx)
	if err != nil {
		return err
	}
	warning, question, done := allOthersWarning, "Sign out every other session?", "Signed out every other session; this CLI stays logged in"
	if scope == cliaccount.EverySession {
		warning, question, done = everySessionWarning, "Sign out every session?", "Signed out every session; this CLI stays logged in"
	}
	if !opts.yes {
		// A prompt nobody can answer would read as a decline and exit 0, with
		// every session still signed in.
		if !confirm.CanPrompt(opts.in) {
			return fmt.Errorf("%s\n\nstdin is not a terminal: confirmation required; pass --yes", warning)
		}
		confirmed, err := confirm.Action(opts.in, opts.out, warning, question)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	if err := service.RevokeOthers(ctx); err != nil {
		return err
	}
	output.Success(opts.out, "%s", done)
	// Ending sessions reaches only the tokens exchanged from them, so say
	// which ones keep working and what ends them. logout revokes the saved
	// login, never a token in the environment.
	if (&config.Config{}).TokenFromEnv() {
		output.Note(opts.out, "Tokens not tied to a session, such as one created in the dashboard, still work. "+
			"This CLI uses the token in %s, which %s doesn't revoke. Revoking platform tokens from the CLI "+
			"is not available yet.",
			config.TokenEnv, cliruntime.CommandPath(opts.deps, "logout"))
		return nil
	}
	output.Note(opts.out, "Tokens not tied to a session, such as one created in the dashboard, "+
		"still work. Revoke the one this CLI uses with %s. Revoking your other platform tokens from the CLI "+
		"is not available yet.",
		cliruntime.CommandPath(opts.deps, "logout"))
	return nil
}
