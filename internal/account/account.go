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

// currentSessionLookupLimit is the page size used to find this CLI's own
// session: the API's page ceiling.
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

// OthersScope says which sessions signing out the others reaches.
type OthersScope int

const (
	// OthersExceptThisLogin keeps the session this CLI is logged in with.
	OthersExceptThisLogin OthersScope = iota
	// EverySession is what a login with no session of its own reaches, such
	// as one with a token issued without a sign-in: there is no session to
	// keep, so all end.
	EverySession
)

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
	current, found, err := currentSession(ctx, authenticated.API)
	if err != nil {
		return err
	}
	if found && current == sessionID {
		return ErrCurrentSession
	}
	if err := authenticated.API.RevokeAccountSession(ctx, sessionID.String()); err != nil {
		if api.Status(err) == http.StatusNotFound {
			return api.WithCause(fmt.Errorf("%w: %s", ErrSessionNotFound, sessionID), err)
		}
		return classify(err, "failed to revoke session "+sessionID.String())
	}
	return nil
}

// OthersScope reports which sessions RevokeOthers would reach: all but this
// CLI's own, or every session when this login has none of its own.
func (s Service) OthersScope(ctx context.Context) (OthersScope, error) {
	authenticated, err := s.sessions.AccountScoped()
	if err != nil {
		return OthersExceptThisLogin, err
	}
	_, found, err := currentSession(ctx, authenticated.API)
	if err != nil {
		return OthersExceptThisLogin, err
	}
	if found {
		return OthersExceptThisLogin, nil
	}
	return EverySession, nil
}

// currentSession finds the session this CLI is logged in with, reading every
// page: the list is ordered by last activity, so an idle login can sit past the
// first. It reports false for a login with no session of its own.
func currentSession(ctx context.Context, client *api.Client) (uuid.UUID, bool, error) {
	for page := 1; ; page++ {
		result, err := client.ListAccountSessions(ctx, page, currentSessionLookupLimit)
		if err != nil {
			return uuid.Nil, false, classify(err, "failed to list sessions")
		}
		for _, session := range result.Sessions {
			if session.IsCurrent {
				return session.Id, true, nil
			}
		}
		if len(result.Sessions) == 0 || (page >= result.TotalPages && page*currentSessionLookupLimit >= result.Total) {
			return uuid.Nil, false, nil
		}
	}
}

// RevokeOthers signs out every session of the account but this CLI's own,
// browser sessions included. Tokens never tied to a session keep working.
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
// the list and for signing out the others, gives a local-mode server's reason
// as it is, and annotates anything else.
func classify(err error, action string) error {
	switch api.Status(err) {
	case http.StatusNotFound:
		return api.WithCause(ErrSessionsUnsupported, err)
	case http.StatusNotImplemented:
		if message := api.Message(err); message != "" {
			return api.WithCause(errors.New(message), err)
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}
