// Package account holds the workflows behind the volcano account subcommands:
// the account's sign-in sessions, browser and CLI alike.
package account

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// currentSessionLookupLimit is how many sessions Revoke reads to find this
// CLI's own: the API's page ceiling.
const currentSessionLookupLimit = 100

// ErrCurrentSession refuses to revoke the session this CLI is logged in with
// through `sessions revoke`, which would leave a saved login the server no
// longer accepts. Logging out ends it and removes the login together.
var ErrCurrentSession = errors.New("that is the session this CLI is logged in with")

// ErrSessionNotFound is a session ID the account has no session under.
var ErrSessionNotFound = errors.New("no session with that ID on this account")

// ErrSessionsUnsupported is an API that predates managing sessions with an
// account token.
var ErrSessionsUnsupported = errors.New("this Volcano API can't manage sessions from the CLI")

// Service performs the account's session workflows.
type Service struct {
	sessions clisession.Factory
}

// NewService returns an account service using the provided runtime dependencies.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// Sessions lists one page of the account's sign-in sessions.
func (s Service) Sessions(ctx context.Context, page, limit int) (*api.AccountSessionsPage, error) {
	authenticated, err := s.sessions.AccountScoped()
	if err != nil {
		return nil, err
	}
	result, err := authenticated.API.ListAccountSessions(ctx, page, limit)
	if err != nil {
		return nil, classify(err, "failed to list sessions")
	}
	return result, nil
}

// Revoke signs one session out, along with the CLI logins made from it. It
// refuses the session this CLI is logged in with: see ErrCurrentSession.
func (s Service) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	authenticated, err := s.sessions.AccountScoped()
	if err != nil {
		return err
	}
	page, err := authenticated.API.ListAccountSessions(ctx, 1, currentSessionLookupLimit)
	if err != nil {
		return classify(err, "failed to list sessions")
	}
	for _, session := range page.Sessions {
		if session.IsCurrent && session.Id == sessionID {
			return ErrCurrentSession
		}
	}
	if err := authenticated.API.RevokeAccountSession(ctx, sessionID); err != nil {
		if api.Status(err) == http.StatusNotFound {
			return api.WithCause(fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID), err)
		}
		return classify(err, "failed to revoke session "+sessionID.String())
	}
	return nil
}

// RevokeOthers signs out every session of the account but this CLI's own,
// browser sessions included.
func (s Service) RevokeOthers(ctx context.Context) error {
	authenticated, err := s.sessions.AccountScoped()
	if err != nil {
		return err
	}
	if err := authenticated.API.RevokeOtherAccountSessions(ctx); err != nil {
		return classify(err, "failed to sign out the other sessions")
	}
	return nil
}

// classify names an API that lacks the session routes, which answers 404 for
// the list and for signing out the others, and annotates anything else.
func classify(err error, action string) error {
	if api.Status(err) == http.StatusNotFound {
		return api.WithCause(ErrSessionsUnsupported, err)
	}
	return fmt.Errorf("%s: %w", action, err)
}
