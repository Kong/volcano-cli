package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListProjects lists one project page for the authenticated user.
func (c *Client) ListProjects(ctx context.Context, page, limit int) (*apiclient.PaginatedProjects, error) {
	resp, err := c.client.ListProjectsWithResponse(ctx, &apiclient.ListProjectsParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// CreateProject creates a project for the authenticated user.
func (c *Client) CreateProject(ctx context.Context, name string) (*apiclient.Project, error) {
	resp, err := c.client.CreateProjectWithResponse(ctx, apiclient.CreateProjectJSONRequestBody{
		Name: strings.TrimSpace(name),
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201, resp.JSON400, resp.JSON403)
}

// ListAnonKeys lists a project's anon keys — the publishable JWTs that go in
// the frontend/SDK Authorization header (the value an app build needs).
func (c *Client) ListAnonKeys(ctx context.Context, projectID uuid.UUID) ([]apiclient.AnonKey, error) {
	resp, err := c.client.ListAnonKeysWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
	if err != nil {
		return nil, err
	}
	if result.Data == nil {
		return nil, nil
	}
	return *result.Data, nil
}

// GetProject returns one project by ID.
func (c *Client) GetProject(ctx context.Context, projectID uuid.UUID) (*apiclient.Project, error) {
	resp, err := c.client.GetProjectWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// DeleteProject starts project deletion.
func (c *Client) DeleteProject(ctx context.Context, projectID uuid.UUID) error {
	resp, err := c.client.DeleteProjectWithResponse(ctx, projectID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON404)
}
