package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListVariables lists one variable page for a project.
func (c *Client) ListVariables(ctx context.Context, projectID uuid.UUID, page, limit int) (*apiclient.PaginatedVariables, error) {
	resp, err := c.client.ListVariablesWithResponse(ctx, projectID, &apiclient.ListVariablesParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// CreateVariable creates a variable in a project.
func (c *Client) CreateVariable(ctx context.Context, projectID uuid.UUID, name, value string) (*apiclient.Variable, error) {
	resp, err := c.client.CreateVariableWithResponse(ctx, projectID, apiclient.CreateVariableJSONRequestBody{
		Name:  strings.TrimSpace(name),
		Value: value,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201, resp.JSON400)
}

// GetVariable returns one variable by name.
func (c *Client) GetVariable(ctx context.Context, projectID uuid.UUID, name string) (*apiclient.Variable, error) {
	resp, err := c.client.GetVariableWithResponse(ctx, projectID, strings.TrimSpace(name))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// UpdateVariable updates one variable by name.
func (c *Client) UpdateVariable(ctx context.Context, projectID uuid.UUID, name, value string) (*apiclient.Variable, error) {
	resp, err := c.client.UpdateVariableWithResponse(ctx, projectID, strings.TrimSpace(name), apiclient.UpdateVariableJSONRequestBody{
		Value: value,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// DeleteVariable deletes one variable by name.
func (c *Client) DeleteVariable(ctx context.Context, projectID uuid.UUID, name string) error {
	resp, err := c.client.DeleteVariableWithResponse(ctx, projectID, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON404)
}
