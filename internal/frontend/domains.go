package frontend

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListDomains returns a frontend's custom domains, oldest first.
func (s Service) ListDomains(ctx context.Context, identifier string) (*apiclient.Frontend, []apiclient.FrontendCustomDomainResponse, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, nil, err
	}
	domains, err := authenticated.API.ListFrontendDomains(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list custom domains for frontend %q: %w", frontend.Name, err)
	}
	return frontend, domains, nil
}

// GetDomain returns one custom domain of a frontend.
func (s Service) GetDomain(ctx context.Context, identifier, domain string) (*apiclient.Frontend, *apiclient.FrontendCustomDomainResponse, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, nil, err
	}
	found, err := authenticated.API.GetFrontendDomain(ctx, authenticated.ProjectID, frontend.Id, domain)
	if err != nil {
		return nil, nil, domainError(frontend, domain, "get", err)
	}
	return frontend, found, nil
}

// AddDomain adds a custom domain beside a frontend's others, or switches the
// TLS mode of one it already has. For an existing domain, input without a
// redirect keeps the domain's, and a redirect without a Status keeps its
// status: the API refuses a request whose redirect differs from the domain's.
func (s Service) AddDomain(
	ctx context.Context, identifier string, input api.FrontendDomainInput,
) (*apiclient.Frontend, *apiclient.FrontendCustomDomainResponse, CustomDomainChange, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, nil, 0, err
	}
	// The server answers a switch and a repeated request alike, so the domain
	// it had before tells them apart.
	previous, err := authenticated.API.GetFrontendDomain(ctx, authenticated.ProjectID, frontend.Id, input.Domain)
	if err != nil && api.Status(err) != http.StatusNotFound {
		return nil, nil, 0, domainError(frontend, input.Domain, "add", err)
	}
	if previous != nil && previous.Redirect != nil {
		switch {
		case input.Redirect == nil:
			input.Redirect = &api.FrontendDomainRedirect{To: previous.Redirect.To, Status: int(previous.Redirect.Status)}
		case input.Redirect.Status == 0:
			input.Redirect = &api.FrontendDomainRedirect{To: input.Redirect.To, Status: int(previous.Redirect.Status)}
		}
	}

	domain, created, err := authenticated.API.CreateFrontendDomain(ctx, authenticated.ProjectID, frontend.Id, input)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to add custom domain %q: %w", input.Domain, err)
	}
	switch {
	case created:
		return frontend, domain, CustomDomainCreated, nil
	case previous != nil && previous.TlsMode != domain.TlsMode:
		return frontend, domain, CustomDomainSwitched, nil
	default:
		return frontend, domain, CustomDomainUnchanged, nil
	}
}

// SetDomainRedirect makes a custom domain redirect, or serve the frontend
// again when redirect is nil. A redirect without a Status keeps the one the
// domain redirects with; the API would reset it to 308.
func (s Service) SetDomainRedirect(
	ctx context.Context, identifier, domain string, redirect *api.FrontendDomainRedirect,
) (*apiclient.Frontend, *apiclient.FrontendCustomDomainResponse, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, nil, err
	}
	if redirect != nil && redirect.Status == 0 {
		current, err := authenticated.API.GetFrontendDomain(ctx, authenticated.ProjectID, frontend.Id, domain)
		if err != nil {
			return nil, nil, domainError(frontend, domain, "update", err)
		}
		if current.Redirect != nil {
			redirect = &api.FrontendDomainRedirect{To: redirect.To, Status: int(current.Redirect.Status)}
		}
	}
	updated, err := authenticated.API.SetFrontendDomainRedirect(ctx, authenticated.ProjectID, frontend.Id, domain, redirect)
	if err != nil {
		return nil, nil, domainError(frontend, domain, "update", err)
	}
	return frontend, updated, nil
}

// DeleteDomain removes one custom domain from a frontend.
func (s Service) DeleteDomain(ctx context.Context, frontendID uuid.UUID, domain string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}
	if err := authenticated.API.DeleteFrontendDomain(ctx, authenticated.ProjectID, frontendID, domain); err != nil {
		return fmt.Errorf("failed to remove custom domain %q: %w", domain, err)
	}
	return nil
}

// domainError keeps the API's message: a server without the domains routes
// also answers 404, and only its message tells that apart from a missing domain.
func domainError(frontend *apiclient.Frontend, domain, action string, err error) error {
	return fmt.Errorf("failed to %s custom domain %q on frontend %q: %w", action, domain, frontend.Name, err)
}
