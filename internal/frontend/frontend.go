package frontend

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Service performs authenticated Volcano frontend workflows.
type Service struct {
	sessions clisession.Factory
}

// CustomDomainEntry contains one frontend custom domain attachment.
type CustomDomainEntry struct {
	Frontend apiclient.Frontend
	Domain   apiclient.FrontendCustomDomainResponse
}

// NewService returns a frontend service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// ListPage returns one frontend page in the current project.
func (s Service) ListPage(ctx context.Context, page, limit int) (*apiclient.PaginatedFrontends, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	frontends, err := authenticated.API.ListFrontends(ctx, authenticated.ProjectID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list frontends: %w", err)
	}
	return frontends, nil
}

// Deploy uploads one frontend source archive and starts a deployment.
func (s Service) Deploy(ctx context.Context, input api.FrontendDeployInput) (*apiclient.Frontend, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	deployed, err := authenticated.API.DeployFrontend(ctx, authenticated.ProjectID, input)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy frontend %q: %w", input.Name, err)
	}
	return deployed, nil
}

// Resolve returns a frontend by name or UUID in the current project.
func (s Service) Resolve(ctx context.Context, identifier string) (*apiclient.Frontend, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	frontend, err := resolveFrontend(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("frontend %q not found", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve frontend %q: %w", identifier, err)
	}
	return frontend, nil
}

// Get returns one frontend by name or UUID.
func (s Service) Get(ctx context.Context, identifier string) (*apiclient.Frontend, error) {
	return s.Resolve(ctx, identifier)
}

// DeleteByID starts deleting one frontend by ID.
func (s Service) DeleteByID(ctx context.Context, frontendID uuid.UUID) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteFrontend(ctx, authenticated.ProjectID, frontendID); err != nil {
		return fmt.Errorf("failed to delete frontend %q: %w", frontendID.String(), err)
	}
	return nil
}

// Redeploy starts a new deployment for the named frontend using the previously
// uploaded archive.
func (s Service) Redeploy(ctx context.Context, identifier string) (*apiclient.Frontend, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	frontend, err := resolveFrontend(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("frontend %q not found", identifier)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve frontend %q: %w", identifier, err)
	}

	redeployed, err := authenticated.API.RedeployFrontend(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to redeploy frontend %q: %w", identifier, err)
	}
	if redeployed.Name == "" {
		// The API layer synthesizes a minimal Frontend when the server
		// returns 2xx with an empty body, so backfill the name from the
		// frontend we just resolved by identifier.
		redeployed.Name = frontend.Name
	}
	return redeployed, nil
}

// ResolveDeployment returns a frontend deployment by ID, paging internally.
func (s Service) ResolveDeployment(ctx context.Context, frontendID uuid.UUID, deploymentID string) (*apiclient.FrontendDeployment, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return nil, api.ErrNotFound
	}

	seen := make(map[uuid.UUID]struct{})
	for page := api.DefaultPage; page < api.DefaultPage+maxResolvePages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		deployments, err := authenticated.API.ListFrontendDeployments(ctx, authenticated.ProjectID, frontendID, page, api.DefaultLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list deployments: %w", err)
		}
		if deployments == nil {
			return nil, api.ErrNotFound
		}
		progressed := false
		for i := range deployments.Data {
			if _, ok := seen[deployments.Data[i].Id]; ok {
				continue
			}
			seen[deployments.Data[i].Id] = struct{}{}
			progressed = true
			if deployments.Data[i].Id.String() == deploymentID {
				return &deployments.Data[i], nil
			}
		}
		if !deployments.HasMore || len(deployments.Data) == 0 || !progressed {
			return nil, api.ErrNotFound
		}
	}
	return nil, fmt.Errorf("gave up looking for deployment after %d pages", maxResolvePages)
}

// LatestDeployment returns the first deployment on the default deployment page.
func (s Service) LatestDeployment(ctx context.Context, frontendID uuid.UUID) (*apiclient.FrontendDeployment, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	deployments, err := authenticated.API.ListFrontendDeployments(ctx, authenticated.ProjectID, frontendID, api.DefaultPage, api.DefaultLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}
	if deployments == nil || len(deployments.Data) == 0 {
		return nil, api.ErrNotFound
	}
	return &deployments.Data[0], nil
}

// RuntimeLogs returns one runtime log search page for a frontend.
func (s Service) RuntimeLogs(ctx context.Context, frontendID uuid.UUID, limit int, cursor string) (*apiclient.LogSearchResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	logs, err := authenticated.API.GetFrontendLogs(ctx, authenticated.ProjectID, frontendID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch runtime logs: %w", err)
	}
	return logs, nil
}

// StreamRuntimeLogs opens a runtime log stream for a frontend.
func (s Service) StreamRuntimeLogs(ctx context.Context, frontendID uuid.UUID, limit int) (*api.ProjectLogStream, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	stream, err := authenticated.API.StreamFrontendLogs(ctx, authenticated.ProjectID, frontendID, limit, "")
	if err != nil {
		return nil, fmt.Errorf("failed to stream runtime logs: %w", err)
	}
	return stream, nil
}

// DeploymentLogs returns one build log search page for a frontend deployment.
func (s Service) DeploymentLogs(ctx context.Context, frontendID, deploymentID uuid.UUID, limit int, cursor string) (*apiclient.LogSearchResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	logs, err := authenticated.API.GetFrontendDeploymentLogs(ctx, authenticated.ProjectID, frontendID, deploymentID, limit, cursor)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch deployment logs: %w", err)
	}
	return logs, nil
}

// StreamDeploymentLogs opens a build log stream for a frontend deployment.
func (s Service) StreamDeploymentLogs(ctx context.Context, frontendID, deploymentID uuid.UUID, limit int) (*api.ProjectLogStream, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	stream, err := authenticated.API.StreamFrontendDeploymentLogs(ctx, authenticated.ProjectID, frontendID, deploymentID, limit, "")
	if err != nil {
		return nil, fmt.Errorf("failed to stream deployment logs: %w", err)
	}
	return stream, nil
}

// CreateCustomDomain attaches a custom domain to a frontend.
func (s Service) CreateCustomDomain(ctx context.Context, identifier string, input api.FrontendCustomDomainInput) (*apiclient.Frontend, *apiclient.FrontendCustomDomainResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, nil, err
	}

	frontend, err := resolveFrontend(ctx, authenticated, identifier)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve frontend %q: %w", identifier, err)
	}
	if frontend == nil {
		return nil, nil, fmt.Errorf("frontend %q not found", identifier)
	}

	domain, err := authenticated.API.CreateFrontendCustomDomain(ctx, authenticated.ProjectID, frontend.Id, input)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create custom domain: %w", err)
	}
	return frontend, domain, nil
}

// GetCustomDomain returns the configured custom domain for a frontend.
func (s Service) GetCustomDomain(ctx context.Context, identifier string) (*apiclient.Frontend, *apiclient.FrontendCustomDomainResponse, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, nil, err
	}

	frontend, err := resolveFrontend(ctx, authenticated, identifier)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve frontend %q: %w", identifier, err)
	}
	if frontend == nil {
		return nil, nil, fmt.Errorf("frontend %q not found", identifier)
	}

	domain, err := authenticated.API.GetFrontendCustomDomain(ctx, authenticated.ProjectID, frontend.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get frontend custom domain: %w", err)
	}
	return frontend, domain, nil
}

// DeleteCustomDomainByID detaches the configured custom domain from a frontend ID.
func (s Service) DeleteCustomDomainByID(ctx context.Context, frontendID uuid.UUID) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteFrontendCustomDomain(ctx, authenticated.ProjectID, frontendID); err != nil {
		return fmt.Errorf("failed to delete frontend custom domain: %w", err)
	}
	return nil
}

// ListCustomDomains lists frontend custom domains in the current project.
func (s Service) ListCustomDomains(ctx context.Context) ([]CustomDomainEntry, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	var entries []CustomDomainEntry
	for page := api.DefaultPage; page < api.DefaultPage+maxResolvePages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		frontends, err := authenticated.API.ListFrontends(ctx, authenticated.ProjectID, page, api.DefaultLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list frontends: %w", err)
		}
		if frontends == nil {
			break
		}
		for i := range frontends.Data {
			domain, err := authenticated.API.GetFrontendCustomDomain(ctx, authenticated.ProjectID, frontends.Data[i].Id)
			if err != nil {
				if api.Status(err) == http.StatusNotFound {
					continue
				}
				return nil, fmt.Errorf("failed to get custom domain for frontend %q: %w", frontends.Data[i].Name, err)
			}
			entries = append(entries, CustomDomainEntry{
				Frontend: frontends.Data[i],
				Domain:   *domain,
			})
		}
		if !frontends.HasMore || len(frontends.Data) == 0 {
			break
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Frontend.Name) < strings.ToLower(entries[j].Frontend.Name)
	})
	return entries, nil
}

// maxResolvePages caps the pagination walk in resolveFrontend so a server
// that keeps reporting HasMore=true cannot hang the CLI indefinitely.
const maxResolvePages = 1000

func resolveFrontend(ctx context.Context, authenticated *clisession.ProjectSession, identifier string) (*apiclient.Frontend, error) {
	target := strings.TrimSpace(identifier)
	if target == "" {
		return nil, errors.New("frontend identifier cannot be empty")
	}

	if id, err := uuid.Parse(target); err == nil {
		frontend, err := authenticated.API.GetFrontend(ctx, authenticated.ProjectID, id)
		if err != nil {
			if api.Status(err) == http.StatusNotFound {
				return nil, api.ErrNotFound
			}
			return nil, err
		}
		return frontend, nil
	}

	seen := make(map[uuid.UUID]struct{})
	for page := api.DefaultPage; page < api.DefaultPage+maxResolvePages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		frontends, err := authenticated.API.ListFrontends(ctx, authenticated.ProjectID, page, api.DefaultLimit)
		if err != nil {
			return nil, err
		}
		if frontends == nil {
			return nil, api.ErrNotFound
		}
		progressed := false
		for i := range frontends.Data {
			if _, ok := seen[frontends.Data[i].Id]; ok {
				continue
			}
			seen[frontends.Data[i].Id] = struct{}{}
			progressed = true
			if frontends.Data[i].Name == target || frontends.Data[i].Id.String() == target {
				return &frontends.Data[i], nil
			}
		}
		// Stop if the server signaled the end, returned no data, or kept
		// returning pages we have already seen — the last guards against a
		// server that keeps reporting HasMore=true without advancing.
		if !frontends.HasMore || len(frontends.Data) == 0 || !progressed {
			return nil, api.ErrNotFound
		}
	}
	return nil, fmt.Errorf("gave up looking for frontend after %d pages", maxResolvePages)
}
