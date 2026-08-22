package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListBackups returns a database's backups and its point-in-time restore window.
func (s Service) ListBackups(ctx context.Context, databaseName string) (*apiclient.DatabaseBackupList, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	backups, err := authenticated.API.ListDatabaseBackups(ctx, authenticated.ProjectID, databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to list backups of database %q: %w", databaseName, err)
	}
	return backups, nil
}

// CreateBackup captures a database as it is now.
func (s Service) CreateBackup(ctx context.Context, databaseName, backupName string) (*apiclient.DatabaseBackup, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	backup, err := authenticated.API.CreateDatabaseBackup(ctx, authenticated.ProjectID, databaseName, backupName)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup %q of database %q: %w", backupName, databaseName, err)
	}
	return backup, nil
}

// GetBackup returns one backup of a database in the current project.
func (s Service) GetBackup(ctx context.Context, databaseName, backupName string) (*apiclient.DatabaseBackup, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	backup, err := authenticated.API.GetDatabaseBackup(ctx, authenticated.ProjectID, databaseName, backupName)
	if err != nil {
		return nil, fmt.Errorf("failed to get backup %q of database %q: %w", backupName, databaseName, err)
	}
	return backup, nil
}

// DeleteBackup deletes one backup of a database in the current project.
func (s Service) DeleteBackup(ctx context.Context, databaseName, backupName string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteDatabaseBackup(ctx, authenticated.ProjectID, databaseName, backupName); err != nil {
		return fmt.Errorf("failed to delete backup %q of database %q: %w", backupName, databaseName, err)
	}
	return nil
}

// GetBackupSchedule returns a database's automated backup schedule.
func (s Service) GetBackupSchedule(ctx context.Context, databaseName string) (*apiclient.DatabaseBackupSchedule, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	schedule, err := authenticated.API.GetDatabaseBackupSchedule(ctx, authenticated.ProjectID, databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to get the backup schedule of database %q: %w", databaseName, err)
	}
	return schedule, nil
}

// SetBackupSchedule replaces a database's automated backup schedule. An empty
// entries slice stops scheduled backups.
func (s Service) SetBackupSchedule(ctx context.Context, databaseName string, entries []apiclient.DatabaseBackupScheduleEntry) (*apiclient.DatabaseBackupSchedule, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	schedule, err := authenticated.API.UpdateDatabaseBackupSchedule(ctx, authenticated.ProjectID, databaseName, entries)
	if err != nil {
		return nil, fmt.Errorf("failed to set the backup schedule of database %q: %w", databaseName, err)
	}
	return schedule, nil
}

// RestoreFromBackup replaces a database's data with a named backup.
func (s Service) RestoreFromBackup(ctx context.Context, databaseName, backupName string) (*apiclient.DatabaseRestore, error) {
	return s.restore(ctx, databaseName, &backupName, nil)
}

// RestoreToPointInTime replaces a database's data with its state at restoreTo.
func (s Service) RestoreToPointInTime(ctx context.Context, databaseName string, restoreTo time.Time) (*apiclient.DatabaseRestore, error) {
	return s.restore(ctx, databaseName, nil, &restoreTo)
}

func (s Service) restore(ctx context.Context, databaseName string, backupName *string, restoreTo *time.Time) (*apiclient.DatabaseRestore, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	restore, err := authenticated.API.CreateDatabaseRestore(ctx, authenticated.ProjectID, databaseName, backupName, restoreTo)
	if err != nil {
		return nil, fmt.Errorf("failed to restore database %q: %w", databaseName, err)
	}
	return restore, nil
}

// ListRestores returns a database's restore history, newest first.
func (s Service) ListRestores(ctx context.Context, databaseName string) (*apiclient.DatabaseRestoreList, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	restores, err := authenticated.API.ListDatabaseRestores(ctx, authenticated.ProjectID, databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to list restores of database %q: %w", databaseName, err)
	}
	return restores, nil
}

// GetRestore returns one restore of a database in the current project.
func (s Service) GetRestore(ctx context.Context, databaseName string, restoreID uuid.UUID) (*apiclient.DatabaseRestore, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	restore, err := authenticated.API.GetDatabaseRestore(ctx, authenticated.ProjectID, databaseName, restoreID)
	if err != nil {
		return nil, fmt.Errorf("failed to get restore %s of database %q: %w", restoreID, databaseName, err)
	}
	return restore, nil
}
