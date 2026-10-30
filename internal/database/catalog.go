package database

import (
	"context"
	"fmt"

	"github.com/Kong/volcano-cli/internal/api"
)

// Regions returns the regions a database can be created in. The route is
// public, so this needs no sign-in.
func (s Service) Regions(ctx context.Context) ([]api.DatabaseRegion, error) {
	client, err := s.publicClient()
	if err != nil {
		return nil, err
	}
	regions, err := client.ListDatabaseRegions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list database regions: %w", err)
	}
	return regions, nil
}

// Versions returns the PostgreSQL versions a database can be created on,
// newest first. The route is public, so this needs no sign-in.
func (s Service) Versions(ctx context.Context) ([]api.PostgresVersion, error) {
	client, err := s.publicClient()
	if err != nil {
		return nil, err
	}
	versions, err := client.ListPostgresVersions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list PostgreSQL versions: %w", err)
	}
	return versions, nil
}

func (s Service) publicClient() (*api.Client, error) {
	cfg, err := s.sessions.Config()
	if err != nil {
		return nil, err
	}
	client, err := s.sessions.APIClient(s.sessions.APIURL(cfg), cfg.Token())
	if err != nil {
		return nil, fmt.Errorf("failed to create api client: %w", err)
	}
	return client, nil
}
