package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListDatabaseBranches returns every branch of a database.
func (c *Client) ListDatabaseBranches(ctx context.Context, projectID uuid.UUID, databaseName string) ([]apiclient.DatabaseBranch, error) {
	resp, err := c.client.ListDatabaseBranchesWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	list, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON503)
	if err != nil {
		return nil, err
	}
	return list.Data, nil
}

// CreateDatabaseBranch forks a branch off a database. The branch returns
// before it is connectable; poll GetDatabaseBranch until it reports active.
func (c *Client) CreateDatabaseBranch(ctx context.Context, projectID uuid.UUID, databaseName, branchName string, ttlSeconds *int64) (*apiclient.DatabaseBranch, error) {
	body := apiclient.CreateDatabaseBranchJSONRequestBody{
		Name:       strings.TrimSpace(branchName),
		TtlSeconds: ttlSeconds,
	}

	resp, err := c.client.CreateDatabaseBranchWithResponse(ctx, projectID, strings.TrimSpace(databaseName), body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON202, resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON503)
}

// GetDatabaseBranch returns one branch by name.
func (c *Client) GetDatabaseBranch(ctx context.Context, projectID uuid.UUID, databaseName, branchName string) (*apiclient.DatabaseBranch, error) {
	resp, err := c.client.GetDatabaseBranchWithResponse(ctx, projectID, strings.TrimSpace(databaseName), strings.TrimSpace(branchName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON503)
}

// UpdateDatabaseBranch re-arms a branch's lifetime from now.
func (c *Client) UpdateDatabaseBranch(ctx context.Context, projectID uuid.UUID, databaseName, branchName string, ttlSeconds int64) (*apiclient.DatabaseBranch, error) {
	body := apiclient.UpdateDatabaseBranchJSONRequestBody{TtlSeconds: ttlSeconds}

	resp, err := c.client.UpdateDatabaseBranchWithResponse(ctx, projectID, strings.TrimSpace(databaseName), strings.TrimSpace(branchName), body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404, resp.JSON409, resp.JSON503)
}

// ResetDatabaseBranch discards a branch's changes and re-forks it from the
// parent's current state. The rewind runs in the background; poll
// GetDatabaseBranch until the branch reports active again.
func (c *Client) ResetDatabaseBranch(ctx context.Context, projectID uuid.UUID, databaseName, branchName string) (*apiclient.DatabaseBranch, error) {
	resp, err := c.client.ResetDatabaseBranchWithResponse(ctx, projectID, strings.TrimSpace(databaseName), strings.TrimSpace(branchName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON202, resp.JSON404, resp.JSON409, resp.JSON503)
}

// ResetDatabaseBranchPassword rotates the branch role's password and returns
// the branch carrying the new connection string.
func (c *Client) ResetDatabaseBranchPassword(ctx context.Context, projectID uuid.UUID, databaseName, branchName string) (*apiclient.DatabaseBranch, error) {
	resp, err := c.client.ResetDatabaseBranchPasswordWithResponse(ctx, projectID, strings.TrimSpace(databaseName), strings.TrimSpace(branchName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON409, resp.JSON503)
}

// DeleteDatabaseBranch deletes one branch by name.
func (c *Client) DeleteDatabaseBranch(ctx context.Context, projectID uuid.UUID, databaseName, branchName string) error {
	resp, err := c.client.DeleteDatabaseBranchWithResponse(ctx, projectID, strings.TrimSpace(databaseName), strings.TrimSpace(branchName))
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON404, resp.JSON503)
}
