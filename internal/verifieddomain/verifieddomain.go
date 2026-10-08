// Package verifieddomain holds the workflows behind the volcano domains
// subcommands.
package verifieddomain

import (
	"context"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Service performs the account's verified domain workflows. Verified domains
// belong to the account, so every call needs an account token.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a verified domain service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// List returns the domains the account has verified.
func (s Service) List(ctx context.Context) ([]apiclient.VerifiedDomain, error) {
	session, err := s.sessions.AccountScoped()
	if err != nil {
		return nil, err
	}
	return session.API.ListVerifiedDomains(ctx)
}

// Verify proves the account owns domain. created is false when the account had
// already verified it.
func (s Service) Verify(ctx context.Context, domain string) (*apiclient.VerifiedDomain, bool, error) {
	session, err := s.sessions.AccountScoped()
	if err != nil {
		return nil, false, err
	}
	return session.API.VerifyDomain(ctx, domain)
}

// Remove gives up the account's ownership of domain.
func (s Service) Remove(ctx context.Context, domain string) error {
	session, err := s.sessions.AccountScoped()
	if err != nil {
		return err
	}
	return session.API.DeleteVerifiedDomain(ctx, domain)
}
