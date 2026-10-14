package api

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListProjectDeploymentsSince lists one page of the project's function and
// frontend deployments created at or after since, newest first.
func (c *Client) ListProjectDeploymentsSince(
	ctx context.Context, projectID uuid.UUID, since time.Time, page, limit int,
) (*apiclient.PaginatedProjectDeployments, error) {
	resp, err := c.client.ListProjectDeploymentsWithResponse(ctx, projectID, &apiclient.ListProjectDeploymentsParams{
		Page:         &page,
		Limit:        &limit,
		CreatedAfter: &since,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON500)
}

// GetProjectSourceExport returns whether the project deploys from Volcano or
// from its connected repository.
func (c *Client) GetProjectSourceExport(ctx context.Context, projectID uuid.UUID) (*apiclient.ProjectSourceExportState, error) {
	resp, err := c.client.GetProjectSourceExportWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON501)
}
