// Package database holds shared types and helpers for the databases subcommands.
package database

import (
	"context"
	"fmt"

	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// Service performs authenticated Volcano database workflows.
type Service struct {
	sessions clisession.Factory
}

// NewService returns a database service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps)}
}

// Create creates a database in the current project.
func (s Service) Create(ctx context.Context, name, region, pgVersion, databaseType string) (*apiclient.Database, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	database, err := authenticated.API.CreateDatabase(ctx, authenticated.ProjectID, name, region, pgVersion, databaseType)
	if err != nil {
		return nil, fmt.Errorf("failed to create database %q: %w", name, err)
	}
	return database, nil
}

// ListPage returns one database page in the current project.
func (s Service) ListPage(ctx context.Context, page, limit int) (*apiclient.PaginatedDatabases, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	databases, err := authenticated.API.ListDatabases(ctx, authenticated.ProjectID, page, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}
	return databases, nil
}

// Get returns one database in the current project by name.
func (s Service) Get(ctx context.Context, name string) (*apiclient.Database, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	database, err := authenticated.API.GetDatabase(ctx, authenticated.ProjectID, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get database %q: %w", name, err)
	}
	return database, nil
}

// Delete deletes one database in the current project by name.
func (s Service) Delete(ctx context.Context, name string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteDatabase(ctx, authenticated.ProjectID, name); err != nil {
		return fmt.Errorf("failed to delete database %q: %w", name, err)
	}
	return nil
}
