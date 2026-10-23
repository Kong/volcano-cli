package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// GetLavaFlowSettings returns whether LavaFlow is on for a project and whether
// the owner's plan allows it.
func (c *Client) GetLavaFlowSettings(ctx context.Context, projectID uuid.UUID) (*apiclient.LavaFlowSettings, error) {
	resp, err := c.client.GetLavaFlowSettingsWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON501, resp.JSON503)
}

// UpdateLavaFlowSettings changes a project's LavaFlow settings. An omitted
// field keeps its saved value.
func (c *Client) UpdateLavaFlowSettings(
	ctx context.Context, projectID uuid.UUID, body apiclient.UpdateLavaFlowSettingsRequest,
) (*apiclient.LavaFlowSettings, error) {
	resp, err := c.client.UpdateLavaFlowSettingsWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON501, resp.JSON503)
}

// ListLavaFlowRuns returns one page of a project's LavaFlow runs, newest
// first.
func (c *Client) ListLavaFlowRuns(
	ctx context.Context, projectID uuid.UUID, params apiclient.ListLavaFlowRunsParams,
) (*apiclient.PaginatedLavaFlowRuns, error) {
	resp, err := c.client.ListLavaFlowRunsWithResponse(ctx, projectID, &params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404, resp.JSON501)
}

// GetLavaFlowRun returns one LavaFlow run.
func (c *Client) GetLavaFlowRun(ctx context.Context, projectID, runID uuid.UUID) (*apiclient.LavaFlowRun, error) {
	resp, err := c.client.GetLavaFlowRunWithResponse(ctx, projectID, runID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON501)
}

// StartLavaFlowRun queues a LavaFlow run for an issue.
func (c *Client) StartLavaFlowRun(ctx context.Context, projectID, issueID uuid.UUID) (*apiclient.LavaFlowRun, error) {
	resp, err := c.client.StartLavaFlowRunWithResponse(ctx, projectID, issueID)
	if err != nil {
		return nil, err
	}
	run, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON202, resp.JSON400, resp.JSON403,
		resp.JSON404, resp.JSON409, resp.JSON429, resp.JSON501, resp.JSON503)
	if resp.StatusCode() == http.StatusTooManyRequests && resp.HTTPResponse != nil {
		// The issue or the project reached its run limit; Retry-After says
		// when a run can start.
		return nil, withRetryAfter(err, resp.HTTPResponse.Header)
	}
	return run, err
}
