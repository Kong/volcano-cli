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

// DeploymentFilter narrows the project's deployment feed.
type DeploymentFilter struct {
	// Type is "function" or "frontend"; empty lists both.
	Type string
	// Kind is "standard" or "durable", for functions only.
	Kind string
	// Search matches a substring of the resource name.
	Search       string
	CreatedAfter *time.Time
}

// ListProjectDeployments lists one page of the project's deployments matching
// filter, newest first.
func (c *Client) ListProjectDeployments(
	ctx context.Context, projectID uuid.UUID, filter DeploymentFilter, cursor string, limit int,
) (*apiclient.PaginatedProjectDeployments, error) {
	// No page number: the feed then pages by cursor, which a deployment landing
	// between requests cannot shift.
	params := &apiclient.ListProjectDeploymentsParams{
		Limit:        &limit,
		CreatedAfter: filter.CreatedAfter,
	}
	if cursor != "" {
		params.Cursor = &cursor
	}
	if filter.Search != "" {
		params.Search = &filter.Search
	}
	if filter.Type != "" {
		resourceType := apiclient.ListProjectDeploymentsParamsResourceType(filter.Type)
		params.ResourceType = &resourceType
	}
	if filter.Kind != "" {
		kind := apiclient.FunctionKindFilter(filter.Kind)
		params.FunctionKind = &kind
	}
	resp, err := c.client.ListProjectDeploymentsWithResponse(ctx, projectID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON500)
}

// SummarizeProjectDeployments counts the outcomes of one resource type's
// deployments matching filter. The route requires filter.Type.
func (c *Client) SummarizeProjectDeployments(
	ctx context.Context, projectID uuid.UUID, filter DeploymentFilter,
) (*apiclient.ProjectDeploymentSummary, error) {
	params := &apiclient.SummarizeProjectDeploymentsParams{
		ResourceType: apiclient.SummarizeProjectDeploymentsParamsResourceType(filter.Type),
		CreatedAfter: filter.CreatedAfter,
	}
	if filter.Search != "" {
		params.Search = &filter.Search
	}
	if filter.Kind != "" {
		kind := apiclient.FunctionKindFilter(filter.Kind)
		params.FunctionKind = &kind
	}
	resp, err := c.client.SummarizeProjectDeploymentsWithResponse(ctx, projectID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON500)
}
