package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListDeployLinks returns the project's deploy links, newest first.
func (c *Client) ListDeployLinks(ctx context.Context, projectID uuid.UUID) (*apiclient.DeployLinksResponse, error) {
	resp, err := c.client.ListDeployLinksWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// CreateDeployLink creates a deploy link for the repository the project is
// connected to. A nil field in request leaves that setting to the API's default.
func (c *Client) CreateDeployLink(
	ctx context.Context, projectID uuid.UUID, request apiclient.CreateDeployLinkRequest,
) (*apiclient.DeployLink, error) {
	resp, err := c.client.CreateDeployLinkWithResponse(ctx, projectID, request)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON422, resp.JSON500, resp.JSON503)
}

// GetDeployLink returns one deploy link.
func (c *Client) GetDeployLink(ctx context.Context, projectID uuid.UUID, linkID string) (*apiclient.DeployLink, error) {
	resp, err := c.client.GetDeployLinkWithResponse(ctx, projectID, linkID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// UpdateDeployLink changes the fields request sets and returns the link.
func (c *Client) UpdateDeployLink(
	ctx context.Context, projectID uuid.UUID, linkID string, request apiclient.UpdateDeployLinkRequest,
) (*apiclient.DeployLink, error) {
	resp, err := c.client.UpdateDeployLinkWithResponse(ctx, projectID, linkID, request)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON422, resp.JSON500, resp.JSON503)
}

// DeleteDeployLink deletes a deploy link and its recorded events.
func (c *Client) DeleteDeployLink(ctx context.Context, projectID uuid.UUID, linkID string) error {
	resp, err := c.client.DeleteDeployLinkWithResponse(ctx, projectID, linkID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// GetDeployLinkStats returns a link's click and deploy counts. A days of zero
// or less leaves the window to the API's default.
func (c *Client) GetDeployLinkStats(
	ctx context.Context, projectID uuid.UUID, linkID string, days int,
) (*apiclient.DeployLinkStats, error) {
	params := &apiclient.GetDeployLinkStatsParams{}
	if days > 0 {
		params.Days = &days
	}
	resp, err := c.client.GetDeployLinkStatsWithResponse(ctx, projectID, linkID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// GetDeployLinkPreview returns what deploying a link would create, with the
// exact commit it is pinned to. The route is public, so the client may carry no
// credential. fresh reads the branch's head again instead of answering from a cache
// that may be a minute old, which is what a retry after commit_changed needs.
func (c *Client) GetDeployLinkPreview(ctx context.Context, linkID string, fresh bool) (*apiclient.DeployLinkPreview, error) {
	params := &apiclient.GetDeployLinkPreviewParams{}
	if fresh {
		params.Fresh = &fresh
	}
	resp, err := c.client.GetDeployLinkPreviewWithResponse(ctx, linkID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON429, resp.JSON500, resp.JSON503)
}
