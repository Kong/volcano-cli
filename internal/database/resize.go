package database

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Sizes are the database sizes, smallest first.
var Sizes = []string{
	string(apiclient.UpdateDatabaseTypeRequestDatabaseTypeVolcanoDbXs),
	string(apiclient.UpdateDatabaseTypeRequestDatabaseTypeVolcanoDbS),
	string(apiclient.UpdateDatabaseTypeRequestDatabaseTypeVolcanoDbM),
	string(apiclient.UpdateDatabaseTypeRequestDatabaseTypeVolcanoDbL),
	string(apiclient.UpdateDatabaseTypeRequestDatabaseTypeVolcanoDbXl),
	string(apiclient.UpdateDatabaseTypeRequestDatabaseTypeVolcanoDb2xl),
}

// ValidateSize refuses a size the platform does not offer.
func ValidateSize(size string) error {
	if !slices.Contains(Sizes, size) {
		return fmt.Errorf("--type must be one of: %s, not %q", strings.Join(Sizes, ", "), size)
	}
	return nil
}

// Resize changes a database's size in the current project.
func (s Service) Resize(ctx context.Context, name, size string) (*apiclient.Database, error) {
	if err := ValidateSize(size); err != nil {
		return nil, err
	}
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	database, err := authenticated.API.UpdateDatabaseType(ctx, authenticated.ProjectID, name, size)
	if err != nil {
		return nil, fmt.Errorf("failed to resize database %q: %w", name, err)
	}
	return database, nil
}
