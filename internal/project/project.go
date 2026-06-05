// Package project resolves and persists the currently selected Volcano project.
package project

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Service performs authenticated Volcano project workflows.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a project service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// List returns the authenticated config and one visible project page.
func (s Service) List(ctx context.Context, page, limit int) (*config.Config, *apiclient.PaginatedProjects, error) {
	authenticated, err := s.sessions.Authenticated()
	if err != nil {
		return nil, nil, err
	}

	projects, err := authenticated.API.ListProjects(ctx, page, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list projects: %w", err)
	}
	return authenticated.Config, projects, nil
}

// Create creates a project for the authenticated user.
func (s Service) Create(ctx context.Context, name string) (*apiclient.Project, error) {
	authenticated, err := s.sessions.Authenticated()
	if err != nil {
		return nil, err
	}

	project, err := authenticated.API.CreateProject(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("failed to create project %q: %w", name, err)
	}
	return project, nil
}

// Get returns one authenticated project by ID.
func (s Service) Get(ctx context.Context, projectID string) (*apiclient.Project, error) {
	authenticated, err := s.sessions.Authenticated()
	if err != nil {
		return nil, err
	}

	id, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return nil, fmt.Errorf("failed to get project: invalid project ID %q: %w", projectID, err)
	}

	project, err := authenticated.API.GetProject(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get project: %w", err)
	}
	return project, nil
}

// Delete starts asynchronous project deletion by ID.
func (s Service) Delete(ctx context.Context, projectID string) error {
	authenticated, err := s.sessions.Authenticated()
	if err != nil {
		return err
	}

	id, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil {
		return fmt.Errorf("failed to delete project: invalid project ID %q: %w", projectID, err)
	}

	if err := authenticated.API.DeleteProject(ctx, id); err != nil {
		return fmt.Errorf("failed to delete project: %w", err)
	}
	return nil
}

// Use sets the active project by exact ID or exact name.
func (s Service) Use(ctx context.Context, identifier string) (*apiclient.Project, error) {
	identifier = strings.TrimSpace(identifier)
	authenticated, err := s.sessions.Authenticated()
	if err != nil {
		return nil, err
	}

	selected, err := resolveProject(ctx, authenticated.API, identifier)
	if err != nil {
		return nil, err
	}
	return selected, saveCurrentProject(authenticated.Config, selected)
}

func resolveProject(ctx context.Context, client *api.Client, identifier string) (*apiclient.Project, error) {
	if id, err := uuid.Parse(identifier); err == nil {
		selected, err := client.GetProject(ctx, id)
		if err == nil {
			return selected, nil
		}
		if api.Status(err) != http.StatusNotFound {
			return nil, fmt.Errorf("failed to get project: %w", err)
		}
	}

	page := api.DefaultPage
	seen := 0
	for {
		projects, err := client.ListProjects(ctx, page, api.DefaultLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list projects: %w", err)
		}

		for i := range projects.Data {
			project := &projects.Data[i]
			if project.Id.String() == identifier || project.Name == identifier {
				return project, nil
			}
		}

		seen += len(projects.Data)
		if !projects.HasMore {
			break
		}
		if len(projects.Data) == 0 {
			return nil, fmt.Errorf("project pagination did not advance at page %d", page)
		}
		if seen >= projects.Total {
			break
		}
		page++
	}

	return nil, fmt.Errorf("project not found: %s", identifier)
}

func saveCurrentProject(cfg *config.Config, project *apiclient.Project) error {
	cfg.CurrentProject = &config.ProjectConfig{
		ID:   project.Id.String(),
		Name: project.Name,
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	return nil
}
