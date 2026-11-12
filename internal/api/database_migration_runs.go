package api

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// MigrationRunInProgressCode is the 409 the API answers when the database
// already has a queued or running migration run.
const MigrationRunInProgressCode = "migration_run_in_progress"

// FeatureUnavailableCode is the 404 the API answers when migration runs are
// not turned on where it runs.
const FeatureUnavailableCode = "feature_unavailable"

// MigrationRunInProgressError is a run the API would not start because RunID
// is already queued or running on the database.
type MigrationRunInProgressError struct {
	Err   *Error
	RunID uuid.UUID
}

func (e *MigrationRunInProgressError) Error() string {
	return fmt.Sprintf("%s (run %s)", e.Err.Error(), e.RunID)
}

func (e *MigrationRunInProgressError) Unwrap() error { return e.Err }

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
		apiErr := &Error{StatusCode: http.StatusConflict, Message: strings.TrimSpace(resp.JSON409.Error), fromAPI: true}
		if resp.JSON409.Code != nil {
			apiErr.Code = *resp.JSON409.Code
		}
		if apiErr.Code == MigrationRunInProgressCode && resp.JSON409.RunId != nil {
			return nil, &MigrationRunInProgressError{Err: apiErr, RunID: *resp.JSON409.RunId}
		}
		return nil, apiErr
	}
	return nil, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON404, resp.JSON413)
}

// GetDatabaseMigrationRun returns one migration run with its files.
func (c *Client) GetDatabaseMigrationRun(
	ctx context.Context, projectID uuid.UUID, databaseName string, runID uuid.UUID,
) (*apiclient.DatabaseMigrationRun, error) {
	resp, err := c.client.GetDatabaseMigrationRunWithResponse(ctx, projectID, strings.TrimSpace(databaseName), runID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}
