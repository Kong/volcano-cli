package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListServiceKeys returns one service-key page for a project.
func (c *Client) ListServiceKeys(ctx context.Context, projectID uuid.UUID, page, limit int) (*apiclient.PaginatedServiceKeys, error) {
	resp, err := c.client.ListServiceKeysWithResponse(ctx, projectID, &apiclient.ListServiceKeysParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// CreateServiceKey creates one service key in a project.
func (c *Client) CreateServiceKey(ctx context.Context, projectID uuid.UUID, name string) (*apiclient.ServiceKey, error) {
	resp, err := c.client.CreateServiceKeyWithResponse(ctx, projectID, apiclient.CreateServiceKeyJSONRequestBody{Name: name})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201)
}

// GetServiceKey returns one service key by ID.
func (c *Client) GetServiceKey(ctx context.Context, projectID, keyID uuid.UUID) (*apiclient.ServiceKey, error) {
	resp, err := c.client.GetServiceKeyWithResponse(ctx, projectID, keyID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}
