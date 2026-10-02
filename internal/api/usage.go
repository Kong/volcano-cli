package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// GetProjectUsage returns aggregate usage for a project. The response also
// contains hourly and daily series; callers choose whether a command contract
// needs to render those details.
func (c *Client) GetProjectUsage(ctx context.Context, projectID uuid.UUID) (*apiclient.ProjectUsageResponse, error) {
	resp, err := c.client.GetProjectUsageWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404)
}
