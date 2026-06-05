package localmode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
)

func (s Service) ensureDefaultDatabase(ctx context.Context, info Info) error {
	name := strings.TrimSpace(info.DefaultDatabaseName)
	region := strings.TrimSpace(info.DefaultDatabaseRegion)
	pgVersion := strings.TrimSpace(info.DefaultDatabasePostgresVersion)
	if name == "" || region == "" || pgVersion == "" {
		return errors.New("local metadata is missing default database fields")
	}

	projectID, err := uuid.Parse(info.ProjectID)
	if err != nil {
		return fmt.Errorf("failed to parse local project ID: %w", err)
	}

	opts := []api.Option{}
	if s.apiHTTPClient != nil {
		opts = append(opts, api.WithHTTPClient(s.apiHTTPClient))
	}
	client, err := api.NewClient(info.APIURL, info.UserToken, opts...)
	if err != nil {
		return err
	}
	_, err = client.CreateDatabase(ctx, projectID, name, region, pgVersion, "")
	if err == nil || api.Status(err) == http.StatusConflict {
		return nil
	}
	return err
}
