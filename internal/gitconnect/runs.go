package gitconnect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

var (
	// ErrRunNotFound indicates the project has no Git deployment run with the
	// ID asked for.
	ErrRunNotFound = errors.New("this project has no Git deployment run with that ID")
	// ErrRunsUnavailable says this API records no Git deployment runs: a
	// platform release older than this CLI.
	ErrRunsUnavailable = errors.New("this Volcano API doesn't record Git deployment runs yet")
)

// RunsQuery selects a page of Git deployment runs. Zero values leave a filter
// out.
type RunsQuery struct {
	Limit  int
	Cursor string
	Status string
	Branch string
}

// ListRuns pages the project's Git deployment runs, newest push first.
//
// Runs come from pushes the GitHub App delivers, so an API without one, such
// as local mode, records none. An empty page there reports
// ErrProviderNotConfigured instead, rather than read as "no push yet".
func (s Service) ListRuns(ctx context.Context, query RunsQuery) (*api.GitDeploymentRunsPage, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	var params apiclient.ListGitDeploymentRunsParams
	if query.Limit > 0 {
		params.Limit = &query.Limit
	}
	if query.Cursor != "" {
		params.Cursor = &query.Cursor
	}
	if query.Status != "" {
		status := apiclient.GitDeploymentRunStatus(query.Status)
		params.Status = &status
	}
	if query.Branch != "" {
		params.Branch = &query.Branch
	}

	page, err := authenticated.API.ListGitDeploymentRuns(ctx, authenticated.ProjectID, params)
	if err != nil {
		if api.RouteMissing(err) {
			return nil, api.WithCause(ErrRunsUnavailable, err)
		}
		return nil, settingsError(authenticated.ProjectID, err, "failed to list Git deployment runs")
	}
	if len(page.Data) == 0 {
		if cannot := s.CanConnect(ctx); cannot != nil {
			return nil, cannot
		}
	}
	return page, nil
}

// GetRun returns one of the project's Git deployment runs.
func (s Service) GetRun(ctx context.Context, runID string) (*api.GitDeploymentRun, error) {
	id, err := uuid.Parse(strings.TrimSpace(runID))
	if err != nil {
		return nil, fmt.Errorf("%q is not a run ID: a run ID is a UUID, such as one 'volcano git runs list' shows", runID)
	}
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}

	run, err := authenticated.API.GetGitDeploymentRun(ctx, authenticated.ProjectID, id)
	if err == nil {
		return run, nil
	}
	switch {
	case api.RouteMissing(err):
		return nil, api.WithCause(ErrRunsUnavailable, err)
	case api.Status(err) == http.StatusNotFound:
		// An API without a GitHub App records no runs, so no ID is found there.
		if cannot := s.CanConnect(ctx); cannot != nil {
			return nil, cannot
		}
		return nil, api.WithCause(fmt.Errorf("%w: %s", ErrRunNotFound, id), err)
	}
	return nil, classify(err, "failed to get Git deployment run "+id.String())
}
