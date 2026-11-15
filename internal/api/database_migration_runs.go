package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// MigrationRunInProgressCode is the 409 the API answers when the database
// already has a queued or running migration run.
const MigrationRunInProgressCode = "migration_run_in_progress"

// MigrationRunInProgressError is a migration request refused because the
// database already has a queued or running run, which RunID names.
type MigrationRunInProgressError struct {
	Err   *Error
	RunID uuid.UUID
}

func (e *MigrationRunInProgressError) Error() string { return e.Err.Error() }

func (e *MigrationRunInProgressError) Unwrap() error { return e.Err }

// GetDatabaseMigrations returns the files a database's migration history
// records and the run that is queued or running, if any.
func (c *Client) GetDatabaseMigrations(ctx context.Context, projectID uuid.UUID, databaseName string) (*apiclient.DatabaseMigrations, error) {
	resp, err := c.client.GetDatabaseMigrationsWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404, resp.JSON409, resp.JSON503)
}

// CreateDatabaseMigrationRun starts a server-side migration run of files on
// databaseName. The server compares them with the database's history and
// applies the pending ones; nothing here connects to the database.
// idempotencyKey makes a retried request return the run the first one started,
// which the API answers with 200 instead of 202.
func (c *Client) CreateDatabaseMigrationRun(
	ctx context.Context, projectID uuid.UUID, databaseName, idempotencyKey string, files []apiclient.DatabaseMigrationFile,
) (*apiclient.DatabaseMigrationRun, error) {
	params := &apiclient.CreateDatabaseMigrationRunParams{}
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		params.IdempotencyKey = &key
	}
	resp, err := c.client.CreateDatabaseMigrationRunWithResponse(ctx, projectID, strings.TrimSpace(databaseName), params,
		apiclient.CreateDatabaseMigrationRunJSONRequestBody{Files: files})
	if err != nil {
		return nil, err
	}
	switch {
	case resp.JSON202 != nil:
		return resp.JSON202, nil
	case resp.JSON200 != nil:
		return resp.JSON200, nil
	case resp.JSON409 != nil:
		return nil, migrationConflictError(resp.JSON409)
	}
	// A run the parser did not accept quotes database errors, which can quote
	// data, so only an error body's own fields reach the error.
	return nil, apiErrorFromGeneratedErrors(resp.StatusCode(), errorFieldsOnly(resp.Body), resp.JSON400, resp.JSON404, resp.JSON413)
}

// BaselineDatabaseMigrations records files in databaseName's migration
// history as applied, without running them. The server skips files it already
// records with the same checksum, and refuses one recorded with another.
func (c *Client) BaselineDatabaseMigrations(
	ctx context.Context, projectID uuid.UUID, databaseName string, files []apiclient.DatabaseMigrationFile,
) (*apiclient.DatabaseMigrationBaseline, error) {
	resp, err := c.client.BaselineDatabaseMigrationsWithResponse(ctx, projectID, strings.TrimSpace(databaseName),
		apiclient.BaselineDatabaseMigrationsJSONRequestBody{Files: files})
	if err != nil {
		return nil, err
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	if resp.JSON409 != nil {
		return nil, migrationConflictError(resp.JSON409)
	}
	return nil, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON404, resp.JSON413, resp.JSON503)
}

// migrationConflictError is the error for a migration 409, naming the run in
// progress when that is the reason.
func migrationConflictError(conflict *apiclient.DatabaseMigrationConflictError) error {
	apiErr := &Error{StatusCode: http.StatusConflict, Message: strings.TrimSpace(conflict.Error), fromAPI: true}
	if conflict.Code != nil {
		apiErr.Code = *conflict.Code
	}
	if apiErr.Code == MigrationRunInProgressCode && conflict.RunId != nil {
		return &MigrationRunInProgressError{Err: apiErr, RunID: *conflict.RunId}
	}
	return apiErr
}

// GetDatabaseMigrationRun returns one migration run with each file's outcome.
func (c *Client) GetDatabaseMigrationRun(
	ctx context.Context, projectID uuid.UUID, databaseName string, runID uuid.UUID,
) (*apiclient.DatabaseMigrationRun, error) {
	resp, err := c.client.GetDatabaseMigrationRunWithResponse(ctx, projectID, strings.TrimSpace(databaseName), runID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), errorFieldsOnly(resp.Body), resp.JSON200, resp.JSON404)
}

// ListDatabaseMigrationRuns returns a database's 50 most recent migration
// runs, newest first, without their files.
func (c *Client) ListDatabaseMigrationRuns(ctx context.Context, projectID uuid.UUID, databaseName string) (*apiclient.DatabaseMigrationRunList, error) {
	resp, err := c.client.ListDatabaseMigrationRunsWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	// A run's error can quote data, as in GetDatabaseMigrationRun.
	return apiResult(resp.StatusCode(), errorFieldsOnly(resp.Body), resp.JSON200, resp.JSON404)
}
