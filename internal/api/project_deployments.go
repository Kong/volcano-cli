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

// ExportProjectSource writes the project's stored source to its empty connected
// repository as the first commit on branch, the production branch the caller
// confirmed. The server refuses with 409 when that branch is no longer current.
func (c *Client) ExportProjectSource(ctx context.Context, projectID uuid.UUID, branch string) (*apiclient.ProjectSourceExport, error) {
	resp, err := c.client.ExportProjectSourceWithResponse(ctx, projectID, apiclient.ExportProjectSourceJSONRequestBody{
		ProductionBranch: branch,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404,
		resp.JSON409, resp.JSON422, resp.JSON429, resp.JSON500, resp.JSON501, resp.JSON503)
}

// CancelProjectSourceExport ends an incomplete source export so direct deploys
// work again. The repository and any commit already pushed are unchanged.
func (c *Client) CancelProjectSourceExport(ctx context.Context, projectID uuid.UUID) error {
	resp, err := c.client.CancelProjectSourceExportWithResponse(ctx, projectID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500, resp.JSON501)
}
