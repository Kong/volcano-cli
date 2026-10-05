package project

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// ListServiceKeys returns one page of service keys for the selected project,
// or for projectID when it is explicitly provided.
func (s Service) ListServiceKeys(ctx context.Context, projectID string, page, limit int) (*apiclient.PaginatedServiceKeys, error) {
	authenticated, id, err := s.selectedProject(projectID, "list service keys")
	if err != nil {
		return nil, err
	}

	keys, err := authenticated.API.ListServiceKeys(ctx, id, page, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to list service keys: %w", err)
	}
	return keys, nil
}

// CreateServiceKey creates a service key in the selected project. An empty
// permissions slice is passed through as omitted so the server's full-access
// default remains authoritative.
func (s Service) CreateServiceKey(ctx context.Context, projectID, name string, permissions []string) (*apiclient.ServiceKey, error) {
	authenticated, id, err := s.selectedProject(projectID, "create service key")
	if err != nil {
		return nil, err
	}

	key, err := authenticated.API.CreateServiceKey(ctx, id, strings.TrimSpace(name), permissions)
	if err != nil {
		return nil, fmt.Errorf("failed to create service key %q: %w", name, err)
	}
	return key, nil
}

// GetServiceKey returns one service key by ID for the selected project.
func (s Service) GetServiceKey(ctx context.Context, projectID, keyID string) (*apiclient.ServiceKey, error) {
	authenticated, id, err := s.selectedProject(projectID, "get service key")
	if err != nil {
		return nil, err
	}

	parsedKeyID, err := uuid.Parse(strings.TrimSpace(keyID))
	if err != nil {
		return nil, fmt.Errorf("failed to get service key: invalid service key ID %q: %w", keyID, err)
	}

	key, err := authenticated.API.GetServiceKey(ctx, id, parsedKeyID)
	if err != nil {
		return nil, fmt.Errorf("failed to get service key: %w", err)
	}
	return key, nil
}

func (s Service) selectedProject(projectID, action string) (*clisession.Session, uuid.UUID, error) {
	authenticated, err := s.sessions.Authenticated()
	if err != nil {
		return nil, uuid.Nil, err
	}

	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		projectID = authenticated.Config.ProjectID()
		if projectID == "" {
			return nil, uuid.Nil, errors.New("failed to " + action + ": no project ID given and no current project selected — pass a project ID or run `volcano use <id-or-name>`")
		}
	}

	id, err := uuid.Parse(projectID)
	if err != nil {
		return nil, uuid.Nil, fmt.Errorf("failed to %s: invalid project ID %q: %w", action, projectID, err)
	}
	return authenticated, id, nil
}
