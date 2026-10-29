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
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// allOthersWarning says what signing out every other session reaches, which
// the name alone undersells: the dashboard in every browser goes with it.
const allOthersWarning = "This signs out every other session on your account: the dashboard in every browser, " +
	"and every other CLI login. This CLI stays logged in."

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
'%s'. A CLI login made from that session stops
working at once.

--all-others signs out every session except this CLI's own, browser sessions
included, so the dashboard asks every browser to sign in again. It asks first;
--yes skips the question.

This CLI's own session ends with '%s', which also removes the
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
				return fmt.Errorf("%w. To end it, run %s, which also removes the login from this machine",
					err, cliruntime.CommandPath(opts.deps, "logout"))
			}
			return err
		}
		output.Success(opts.out, "Signed out session %s", opts.sessionID)
		return nil
	}

	if !opts.yes {
		// A prompt nobody can answer would read as a decline and exit 0, with
		// every session still signed in.
		if !confirm.CanPrompt(opts.in) {
			return fmt.Errorf("%s\n\nstdin is not a terminal: confirmation required; pass --yes", allOthersWarning)
		}
		confirmed, err := confirm.Action(opts.in, opts.out, allOthersWarning, "Sign out every other session?")
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
	output.Success(opts.out, "Signed out every other session; this CLI stays logged in")
	return nil
}
