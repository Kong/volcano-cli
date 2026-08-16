package database

import (
	"context"
	"fmt"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListBranches returns every branch of a database in the current project.
func (s Service) ListBranches(ctx context.Context, databaseName string) ([]apiclient.DatabaseBranch, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	branches, err := authenticated.API.ListDatabaseBranches(ctx, authenticated.ProjectID, databaseName)
	if err != nil {
		return nil, fmt.Errorf("failed to list branches of database %q: %w", databaseName, err)
	}
	return branches, nil
}

// CreateBranch forks a branch off a database in the current project.
func (s Service) CreateBranch(ctx context.Context, databaseName, branchName string, ttlSeconds *int64) (*apiclient.DatabaseBranch, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	branch, err := authenticated.API.CreateDatabaseBranch(ctx, authenticated.ProjectID, databaseName, branchName, ttlSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to create branch %q of database %q: %w", branchName, databaseName, err)
	}
	return branch, nil
}

// GetBranch returns one branch of a database in the current project.
func (s Service) GetBranch(ctx context.Context, databaseName, branchName string) (*apiclient.DatabaseBranch, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	branch, err := authenticated.API.GetDatabaseBranch(ctx, authenticated.ProjectID, databaseName, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to get branch %q of database %q: %w", branchName, databaseName, err)
	}
	return branch, nil
}

// ExtendBranch re-arms a branch's lifetime from now.
func (s Service) ExtendBranch(ctx context.Context, databaseName, branchName string, ttlSeconds int64) (*apiclient.DatabaseBranch, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	branch, err := authenticated.API.UpdateDatabaseBranch(ctx, authenticated.ProjectID, databaseName, branchName, ttlSeconds)
	if err != nil {
		return nil, fmt.Errorf("failed to extend branch %q of database %q: %w", branchName, databaseName, err)
	}
	return branch, nil
}

// ResetBranch discards a branch's changes and re-forks it from the parent.
func (s Service) ResetBranch(ctx context.Context, databaseName, branchName string) (*apiclient.DatabaseBranch, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	branch, err := authenticated.API.ResetDatabaseBranch(ctx, authenticated.ProjectID, databaseName, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to reset branch %q of database %q: %w", branchName, databaseName, err)
	}
	return branch, nil
}

// RotateBranchPassword rotates the branch role's password, invalidating the
// previous connection string.
func (s Service) RotateBranchPassword(ctx context.Context, databaseName, branchName string) (*apiclient.DatabaseBranch, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	branch, err := authenticated.API.ResetDatabaseBranchPassword(ctx, authenticated.ProjectID, databaseName, branchName)
	if err != nil {
		return nil, fmt.Errorf("failed to rotate the password for branch %q of database %q: %w", branchName, databaseName, err)
	}
	return branch, nil
}

// DeleteBranch deletes one branch of a database in the current project.
func (s Service) DeleteBranch(ctx context.Context, databaseName, branchName string) error {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return err
	}

	if err := authenticated.API.DeleteDatabaseBranch(ctx, authenticated.ProjectID, databaseName, branchName); err != nil {
		return fmt.Errorf("failed to delete branch %q of database %q: %w", branchName, databaseName, err)
	}
	return nil
}
