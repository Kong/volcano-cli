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

// AddDomain adds a custom domain beside a frontend's others.
func (s Service) AddDomain(ctx context.Context, identifier string, input api.FrontendDomainInput) (*apiclient.Frontend, *apiclient.FrontendCustomDomainResponse, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, nil, err
	}
	created, err := authenticated.API.CreateFrontendDomain(ctx, authenticated.ProjectID, frontend.Id, input)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to add custom domain %q: %w", input.Domain, err)
	}
	return frontend, created, nil
}

// SetDomainRedirect makes a custom domain redirect, or serve the frontend
// again when redirect is nil.
func (s Service) SetDomainRedirect(
	ctx context.Context, identifier, domain string, redirect *api.FrontendDomainRedirect,
) (*apiclient.Frontend, *apiclient.FrontendCustomDomainResponse, error) {
	authenticated, frontend, err := s.currentFrontend(ctx, identifier)
	if err != nil {
		return nil, nil, err
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

// domainError names the domain a 404 is about; the frontend was found first.
func domainError(frontend *apiclient.Frontend, domain, action string, err error) error {
	if api.Status(err) == http.StatusNotFound {
		return fmt.Errorf("frontend %q has no custom domain %q", frontend.Name, domain)
	}
	return fmt.Errorf("failed to %s custom domain %q: %w", action, domain, err)
}
