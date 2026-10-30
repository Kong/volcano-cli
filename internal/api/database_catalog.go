package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// DatabaseRegion is a region a database can be created in.
type DatabaseRegion struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PostgresVersion is a PostgreSQL major version a database can be created on.
type PostgresVersion struct {
	Version     string `json:"version"`
	Name        string `json:"name"`
	Default     bool   `json:"default"`
	Recommended bool   `json:"recommended"`
	Deprecated  bool   `json:"deprecated"`
}

// ListDatabaseRegions returns the regions a database can be created in.
func (c *Client) ListDatabaseRegions(ctx context.Context) ([]DatabaseRegion, error) {
	resp, err := c.client.ListDatabaseRegionsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	listed, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
	if err != nil {
		return nil, err
	}
	regions := make([]DatabaseRegion, 0, len(*listed))
	for _, region := range *listed {
		regions = append(regions, DatabaseRegion{ID: deref(region.Id), Name: deref(region.Name)})
	}
	return regions, nil
}

// ListPostgresVersions returns the PostgreSQL versions a database can be
// created on, newest first.
func (c *Client) ListPostgresVersions(ctx context.Context) ([]PostgresVersion, error) {
	resp, err := c.client.ListPostgresVersionsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	listed, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
	if err != nil {
		return nil, err
	}
	versions := make([]PostgresVersion, 0, len(*listed))
	for _, version := range *listed {
		versions = append(versions, PostgresVersion{
			Version:     deref(version.Version),
			Name:        deref(version.Name),
			Default:     deref(version.Default),
			Recommended: deref(version.Recommended),
			Deprecated:  deref(version.Deprecated),
		})
	}
	return versions, nil
}

// ListFunctionRegions returns the regions functions can be deployed to.
func (c *Client) ListFunctionRegions(ctx context.Context) ([]apiclient.FunctionRegion, error) {
	resp, err := c.client.ListFunctionRegionsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	regions, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200)
	if err != nil {
		return nil, err
	}
	return *regions, nil
}

// UpdateDatabaseType changes a database's size.
func (c *Client) UpdateDatabaseType(ctx context.Context, projectID uuid.UUID, databaseName, databaseType string) (*apiclient.Database, error) {
	resp, err := c.client.UpdateDatabaseTypeWithResponse(ctx, projectID, strings.TrimSpace(databaseName), apiclient.UpdateDatabaseTypeJSONRequestBody{
		DatabaseType: apiclient.UpdateDatabaseTypeRequestDatabaseType(databaseType),
	})
	if err != nil {
		return nil, err
	}
	// Error fields only: a success body that failed to parse carries the
	// connection string, and with it the database password.
	return apiResult(resp.StatusCode(), errorFieldsOnly(resp.Body), resp.JSON200)
}

// PingDatabase runs the database API's connectivity probe, a SELECT 1, against
// a database, or against one of its branches when branchName is set. The
// client must carry a credential the database API accepts, such as a service
// key.
func (c *Client) PingDatabase(ctx context.Context, databaseName, branchName string) error {
	databaseName, branchName = strings.TrimSpace(databaseName), strings.TrimSpace(branchName)
	if branchName == "" {
		resp, err := c.client.QueryDatabasePingWithResponse(ctx, databaseName)
		if err != nil {
			return err
		}
		return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON429)
	}
	resp, err := c.client.QueryDatabaseBranchPingWithResponse(ctx, databaseName, branchName)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON429, resp.JSON503)
}

// deref returns what value points to, or the zero value for an absent field.
func deref[T any](value *T) T {
	if value == nil {
		var zero T
		return zero
	}
	return *value
}
