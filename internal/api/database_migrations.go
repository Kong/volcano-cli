package api

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// MigrationRunInProgressError is a migration request refused because the
// database already has a queued or running run, which RunID names.
type MigrationRunInProgressError struct {
	Err   *Error
	RunID uuid.UUID
}

func (e *MigrationRunInProgressError) Error() string { return e.Err.Error() }

func (e *MigrationRunInProgressError) Unwrap() error { return e.Err }

// CodeMigrationRunInProgress is the code of a refusal while another migration
// run on the database is queued or running.
const CodeMigrationRunInProgress = "migration_run_in_progress"

// GetDatabaseMigrations returns the files a database's migration history
// records and the run that is queued or running, if any.
func (c *Client) GetDatabaseMigrations(ctx context.Context, projectID uuid.UUID, databaseName string) (*apiclient.DatabaseMigrations, error) {
	resp, err := c.client.GetDatabaseMigrationsWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON409, resp.JSON503)
}

// BaselineDatabaseMigrations records files in a database's migration history
// as applied without running them.
func (c *Client) BaselineDatabaseMigrations(ctx context.Context, projectID uuid.UUID, databaseName string, files []apiclient.DatabaseMigrationFile) (*apiclient.DatabaseMigrationBaseline, error) {
	body := apiclient.BaselineDatabaseMigrationsJSONRequestBody{Files: files}
	resp, err := c.client.BaselineDatabaseMigrationsWithResponse(ctx, projectID, strings.TrimSpace(databaseName), body)
	if err != nil {
		return nil, err
	}
	if resp.JSON409 != nil {
		return nil, migrationConflictError(resp.StatusCode(), resp.JSON409)
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404, resp.JSON413, resp.JSON503)
}

// CreateDatabaseMigrationRun queues a run that applies the files the
// database's history does not record yet. A repeat with the same
// idempotencyKey returns the run the first request started.
func (c *Client) CreateDatabaseMigrationRun(ctx context.Context, projectID uuid.UUID, databaseName, idempotencyKey string, files []apiclient.DatabaseMigrationFile) (*apiclient.DatabaseMigrationRun, error) {
	params := &apiclient.CreateDatabaseMigrationRunParams{}
	if idempotencyKey != "" {
		params.IdempotencyKey = &idempotencyKey
	}
	body := apiclient.CreateDatabaseMigrationRunJSONRequestBody{Files: files}
	resp, err := c.client.CreateDatabaseMigrationRunWithResponse(ctx, projectID, strings.TrimSpace(databaseName), params, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON409 != nil {
		return nil, migrationConflictError(resp.StatusCode(), resp.JSON409)
	}
	run := resp.JSON202
	if run == nil {
		run = resp.JSON200
	}
	return apiResult(resp.StatusCode(), resp.Body, run, resp.JSON400, resp.JSON404, resp.JSON413)
}

// ListDatabaseMigrationRuns returns a database's 50 most recent migration
// runs, newest first, without their files.
func (c *Client) ListDatabaseMigrationRuns(ctx context.Context, projectID uuid.UUID, databaseName string) (*apiclient.DatabaseMigrationRunList, error) {
	resp, err := c.client.ListDatabaseMigrationRunsWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// GetDatabaseMigrationRun returns one migration run with each file's outcome.
func (c *Client) GetDatabaseMigrationRun(ctx context.Context, projectID uuid.UUID, databaseName string, runID uuid.UUID) (*apiclient.DatabaseMigrationRun, error) {
	resp, err := c.client.GetDatabaseMigrationRunWithResponse(ctx, projectID, strings.TrimSpace(databaseName), runID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}

// migrationConflictError keeps the run a migration_run_in_progress refusal
// names, so the caller can say which run to wait for.
func migrationConflictError(statusCode int, conflict *apiclient.DatabaseMigrationConflictError) error {
	apiErr := &Error{StatusCode: statusCode, Message: strings.TrimSpace(conflict.Error), fromAPI: true}
	if conflict.Code != nil {
		apiErr.Code = *conflict.Code
	}
	if apiErr.Code == CodeMigrationRunInProgress && conflict.RunId != nil {
		return &MigrationRunInProgressError{Err: apiErr, RunID: *conflict.RunId}
	}
	return apiErr
}
