// Package variable holds shared types and helpers for the variables subcommands.
package variable

import (
	"context"
	"fmt"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Service performs authenticated Volcano variable workflows.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a variable service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// ListPage returns one variable page in the current project.
func (s Service) ListPage(ctx context.Context, page, limit int) (*apiclient.PaginatedVariables, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	variables, err := authenticated.API.ListVariables(ctx, authenticated.ProjectID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list variables: %w", err)
	}
	return variables, nil
}

// Create creates a variable in the current project.
func (s Service) Create(ctx context.Context, name, value string) (*apiclient.Variable, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	variable, err := authenticated.API.CreateVariable(ctx, authenticated.ProjectID, name, value)
	if err != nil {
		return nil, fmt.Errorf("failed to create variable %q: %w", name, err)
	}
	return variable, nil
}

// Get returns one variable in the current project by name.
func (s Service) Get(ctx context.Context, name string) (*apiclient.Variable, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	variable, err := authenticated.API.GetVariable(ctx, authenticated.ProjectID, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get variable %q: %w", name, err)
	}
	return variable, nil
}

// Update updates one variable in the current project.
func (s Service) Update(ctx context.Context, name, value string) (*apiclient.Variable, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	variable, err := authenticated.API.UpdateVariable(ctx, authenticated.ProjectID, name, value)
	if err != nil {
		return nil, fmt.Errorf("failed to update variable %q: %w", name, err)
	}
	return variable, nil
}

// Delete deletes one variable in the current project by name.
func (s Service) Delete(ctx context.Context, name string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteVariable(ctx, authenticated.ProjectID, name); err != nil {
		return fmt.Errorf("failed to delete variable %q: %w", name, err)
	}
	return nil
}
