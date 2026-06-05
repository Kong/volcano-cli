package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/apiclient/common"
)

// ListDatabases lists one database page for a project.
func (c *Client) ListDatabases(ctx context.Context, projectID uuid.UUID, page, limit int) (*apiclient.PaginatedDatabases, error) {
	resp, err := c.client.ListDatabasesWithResponse(ctx, projectID, &apiclient.ListDatabasesParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// CreateDatabase creates a database in a project.
func (c *Client) CreateDatabase(ctx context.Context, projectID uuid.UUID, name, region, pgVersion, databaseType string) (*apiclient.Database, error) {
	body := apiclient.CreateDatabaseJSONRequestBody{
		Name:      strings.TrimSpace(name),
		Region:    common.CreateDatabaseRequestRegion(strings.TrimSpace(region)),
		PgVersion: common.CreateDatabaseRequestPgVersion(strings.TrimSpace(pgVersion)),
	}
	if databaseType := strings.TrimSpace(databaseType); databaseType != "" {
		typedDatabaseType := common.CreateDatabaseRequestDatabaseType(databaseType)
		body.DatabaseType = &typedDatabaseType
	}

	resp, err := c.client.CreateDatabaseWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201, resp.JSON403)
}

// GetDatabase returns one database by name.
func (c *Client) GetDatabase(ctx context.Context, projectID uuid.UUID, databaseName string) (*apiclient.Database, error) {
	resp, err := c.client.GetDatabaseWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
}

// DeleteDatabase deletes one database by name.
func (c *Client) DeleteDatabase(ctx context.Context, projectID uuid.UUID, databaseName string) error {
	resp, err := c.client.DeleteDatabaseWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body)
}
