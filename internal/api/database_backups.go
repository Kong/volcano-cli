package api

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListDatabaseBackups returns a database's backups along with the window a
// point-in-time restore may target.
func (c *Client) ListDatabaseBackups(ctx context.Context, projectID uuid.UUID, databaseName string) (*apiclient.DatabaseBackupList, error) {
	resp, err := c.client.ListDatabaseBackupsWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404, resp.JSON503)
}

// CreateDatabaseBackup captures the database as it is now.
func (c *Client) CreateDatabaseBackup(ctx context.Context, projectID uuid.UUID, databaseName, backupName string) (*apiclient.DatabaseBackup, error) {
	body := apiclient.CreateDatabaseBackupJSONRequestBody{Name: strings.TrimSpace(backupName)}

	resp, err := c.client.CreateDatabaseBackupWithResponse(ctx, projectID, strings.TrimSpace(databaseName), body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201, resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON503)
}

// GetDatabaseBackup returns one backup by name.
func (c *Client) GetDatabaseBackup(ctx context.Context, projectID uuid.UUID, databaseName, backupName string) (*apiclient.DatabaseBackup, error) {
	resp, err := c.client.GetDatabaseBackupWithResponse(ctx, projectID, strings.TrimSpace(databaseName), strings.TrimSpace(backupName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404, resp.JSON503)
}

// DeleteDatabaseBackup deletes one backup by name.
func (c *Client) DeleteDatabaseBackup(ctx context.Context, projectID uuid.UUID, databaseName, backupName string) error {
	resp, err := c.client.DeleteDatabaseBackupWithResponse(ctx, projectID, strings.TrimSpace(databaseName), strings.TrimSpace(backupName))
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON403, resp.JSON404, resp.JSON503)
}

// GetDatabaseBackupSchedule returns the database's automated backup schedule.
func (c *Client) GetDatabaseBackupSchedule(ctx context.Context, projectID uuid.UUID, databaseName string) (*apiclient.DatabaseBackupSchedule, error) {
	resp, err := c.client.GetDatabaseBackupScheduleWithResponse(ctx, projectID, strings.TrimSpace(databaseName))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404, resp.JSON503)
}

// UpdateDatabaseBackupSchedule replaces the schedule wholesale. An empty entries
// slice stops scheduled backups. The API clamps retention to the plan's, so the
// schedule it returns can differ from the one sent.
func (c *Client) UpdateDatabaseBackupSchedule(ctx context.Context, projectID uuid.UUID, databaseName string, entries []apiclient.DatabaseBackupScheduleEntry) (*apiclient.DatabaseBackupSchedule, error) {
	if entries == nil {
		entries = []apiclient.DatabaseBackupScheduleEntry{}
	}
	body := apiclient.UpdateDatabaseBackupScheduleJSONRequestBody{Entries: entries}

	resp, err := c.client.UpdateDatabaseBackupScheduleWithResponse(ctx, projectID, strings.TrimSpace(databaseName), body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON503)
}

// CreateDatabaseRestore replaces a database's data, either from a named backup
// or from its state at a point in time. Exactly one of backupName and restoreTo
// must be set. The restore returns before it finishes; the database is not
// connectable until it reports completed.
func (c *Client) CreateDatabaseRestore(ctx context.Context, projectID uuid.UUID, databaseName string, backupName *string, restoreTo *time.Time) (*apiclient.DatabaseRestore, error) {
	body := apiclient.CreateDatabaseRestoreJSONRequestBody{
		BackupName: backupName,
		RestoreTo:  restoreTo,
	}

	resp, err := c.client.CreateDatabaseRestoreWithResponse(ctx, projectID, strings.TrimSpace(databaseName), body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON202, resp.JSON400, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON503)
}
