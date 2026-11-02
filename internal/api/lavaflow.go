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

// LavaFlow's routes are top-level and name one project each: project_id is
// a query parameter on the reads and a body field on the settings save. A
// project the caller can't use answers 404 like one that doesn't exist.

// GetLavaflowSettings returns a project's LavaFlow settings.
func (c *Client) GetLavaflowSettings(ctx context.Context, projectID uuid.UUID) (*apiclient.LavaflowSettings, error) {
	resp, err := c.client.GetLavaflowSettingsWithResponse(ctx, &apiclient.GetLavaflowSettingsParams{ProjectId: projectID})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// UpdateLavaflowSettings changes a project's LavaFlow settings, naming the
// project in the body. An omitted field keeps its value. Only the project's
// owner may, with a platform token: a project access token gets 403
// person_required.
func (c *Client) UpdateLavaflowSettings(
	ctx context.Context, projectID uuid.UUID, body apiclient.LavaflowSettingsUpdate,
) (*apiclient.LavaflowSettings, error) {
	body.ProjectId = projectID
	resp, err := c.client.UpdateLavaflowSettingsWithResponse(ctx, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// GetLavaflowStats returns the issues LavaFlow took on and the fix pull
// requests it opened per day over the last days days, today included.
func (c *Client) GetLavaflowStats(ctx context.Context, projectID uuid.UUID, days int) (*apiclient.LavaflowStats, error) {
	resp, err := c.client.GetLavaflowStatsWithResponse(ctx, &apiclient.GetLavaflowStatsParams{ProjectId: projectID, Days: &days})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// ListLavaflowResources returns every function, frontend and durable function
// in a project with what LavaFlow did for it over the last days days.
func (c *Client) ListLavaflowResources(ctx context.Context, projectID uuid.UUID, days int) (*apiclient.LavaflowResourceList, error) {
	resp, err := c.client.ListLavaflowResourcesWithResponse(ctx, &apiclient.ListLavaflowResourcesParams{ProjectId: projectID, Days: &days})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// ListLavaflowRuns returns one page of a project's LavaFlow runs, newest
// first. A cursor continues only the same project's list for the same caller.
func (c *Client) ListLavaflowRuns(
	ctx context.Context, projectID uuid.UUID, params apiclient.ListLavaflowRunsParams,
) (*apiclient.PaginatedLavaflowRuns, error) {
	params.ProjectId = projectID
	resp, err := c.client.ListLavaflowRunsWithResponse(ctx, &params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// GetLavaflowRun returns one LavaFlow run with its diagnosis, diff and
// review. It takes the access the project's log routes take.
func (c *Client) GetLavaflowRun(ctx context.Context, projectID, runID uuid.UUID) (*apiclient.LavaflowRun, error) {
	resp, err := c.client.GetLavaflowRunWithResponse(ctx, runID, &apiclient.GetLavaflowRunParams{ProjectId: projectID})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}
