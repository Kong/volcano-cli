package project

import (
	"context"
	"fmt"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Usage returns aggregate usage for the selected project, or projectID when it
// is explicitly provided.
func (s Service) Usage(ctx context.Context, projectID string) (*apiclient.ProjectUsageResponse, error) {
	authenticated, id, err := s.selectedProject(projectID, "get project usage")
	if err != nil {
		return nil, err
	}

	usage, err := authenticated.API.GetProjectUsage(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get project usage: %w", err)
	}
	return usage, nil
}
