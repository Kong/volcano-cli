package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListProjectCapabilities returns which features are available for a project.
// Callers read it to decide what to offer instead of probing a route.
func (c *Client) ListProjectCapabilities(ctx context.Context, projectID uuid.UUID) (*apiclient.CapabilityList, error) {
	resp, err := c.client.ListProjectCapabilitiesWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON404)
}

// GetLavaflowSettings returns a project's LavaFlow settings.
func (c *Client) GetLavaflowSettings(ctx context.Context, projectID uuid.UUID) (*apiclient.LavaflowSettings, error) {
	resp, err := c.client.GetLavaflowSettingsWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404)
}

// UpdateLavaflowSettings changes a project's LavaFlow settings. An omitted
// field keeps its value. Only the project's owner may, with a platform token:
// a project access token gets 403 person_required.
func (c *Client) UpdateLavaflowSettings(
	ctx context.Context, projectID uuid.UUID, body apiclient.LavaflowSettingsUpdate,
) (*apiclient.LavaflowSettings, error) {
	resp, err := c.client.UpdateLavaflowSettingsWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON503)
}

// GetLavaflowStats returns the issues LavaFlow took on and the fix pull
// requests it opened per day over the last days days, today included.
func (c *Client) GetLavaflowStats(ctx context.Context, projectID uuid.UUID, days int) (*apiclient.LavaflowStats, error) {
	resp, err := c.client.GetLavaflowStatsWithResponse(ctx, projectID, &apiclient.GetLavaflowStatsParams{Days: &days})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// ListLavaflowResources returns every function, frontend and durable function
// in a project with what LavaFlow did for it over the last days days.
func (c *Client) ListLavaflowResources(ctx context.Context, projectID uuid.UUID, days int) (*apiclient.LavaflowResourceList, error) {
	resp, err := c.client.ListLavaflowResourcesWithResponse(ctx, projectID, &apiclient.ListLavaflowResourcesParams{Days: &days})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// ListLavaflowRuns returns one page of a project's LavaFlow runs, newest
// first.
func (c *Client) ListLavaflowRuns(
	ctx context.Context, projectID uuid.UUID, params apiclient.ListLavaflowRunsParams,
) (*apiclient.PaginatedLavaflowRuns, error) {
	resp, err := c.client.ListLavaflowRunsWithResponse(ctx, projectID, &params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// GetLavaflowRun returns one LavaFlow run with its diagnosis, diff and
// review. It takes the access the project's log routes take.
func (c *Client) GetLavaflowRun(ctx context.Context, projectID, runID uuid.UUID) (*apiclient.LavaflowRun, error) {
	resp, err := c.client.GetLavaflowRunWithResponse(ctx, projectID, runID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404)
}
