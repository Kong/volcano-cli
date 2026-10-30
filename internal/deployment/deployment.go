// Package deployment lists, finds and summarizes the current project's function
// and frontend deployments.
package deployment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

const (
	// pageLimit is the feed route's largest page.
	pageLimit = 100
	// maxFindPages bounds a search for one deployment by ID. The feed has no
	// read by ID, so finding one means paging; 50 pages is the newest 5000.
	maxFindPages = 50
)

// ErrNotFound reports a deployment ID that is not among the project's
// deployments the search reached.
var ErrNotFound = errors.New("deployment not found")

// Service reads the current project's deployments.
type Service struct {
	sessions clisession.Factory
	pinned   *pinnedProject
}

// pinnedProject resolves the project once per service, so every page of one
// listing comes from the same project.
type pinnedProject struct {
	once    sync.Once
	session *clisession.ProjectSession
	err     error
}

// NewService returns a deployment service.
func NewService(deps cliruntime.Deps) Service {
	return Service{sessions: clisession.NewFactory(deps), pinned: &pinnedProject{}}
}

func (s Service) current() (*clisession.ProjectSession, error) {
	s.pinned.once.Do(func() {
		s.pinned.session, s.pinned.err = s.sessions.CurrentProject()
	})
	return s.pinned.session, s.pinned.err
}

// Validate checks a filter before anything is sent. requireType is set for a
// summary, which covers one resource type.
func Validate(filter api.DeploymentFilter, requireType bool) error {
	switch filter.Type {
	case "function", "frontend":
	case "":
		if requireType {
			return errors.New("--type must be function or frontend")
		}
	default:
		return fmt.Errorf("--type must be function or frontend, not %q", filter.Type)
	}
	switch {
	case filter.Kind != "" && filter.Kind != "standard" && filter.Kind != "durable":
		return fmt.Errorf("--kind must be standard or durable, not %q", filter.Kind)
	case filter.Kind != "" && filter.Type == "frontend":
		return errors.New("--kind applies only to functions")
	}
	return nil
}

// List returns up to limit deployments matching filter, newest first, and
// whether more match.
func (s Service) List(ctx context.Context, filter api.DeploymentFilter, limit int) ([]apiclient.ProjectDeployment, bool, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, false, err
	}
	// A cursor is bound to its page size, so every page asks for the same one.
	size := min(limit, pageLimit)
	var deployments []apiclient.ProjectDeployment
	cursor := ""
	for len(deployments) < limit {
		resp, err := authenticated.API.ListProjectDeployments(ctx, authenticated.ProjectID, filter, cursor, size)
		if err != nil {
			return nil, false, fmt.Errorf("failed to list deployments: %w", err)
		}
		deployments = append(deployments, resp.Data...)
		if !resp.HasMore || resp.NextCursor == nil || len(resp.Data) == 0 {
			return truncate(deployments, limit), len(deployments) > limit, nil
		}
		cursor = *resp.NextCursor
	}
	return truncate(deployments, limit), true, nil
}

func truncate(deployments []apiclient.ProjectDeployment, limit int) []apiclient.ProjectDeployment {
	if len(deployments) > limit {
		return deployments[:limit]
	}
	return deployments
}

// Find returns one deployment by ID. The feed has no read by ID, so this pages
// it, newest first, and gives up after the newest maxFindPages pages.
func (s Service) Find(ctx context.Context, id string) (*apiclient.ProjectDeployment, error) {
	deploymentID, err := uuid.Parse(strings.TrimSpace(id))
	if err != nil {
		return nil, fmt.Errorf("%q is not a deployment ID", id)
	}
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}
	cursor := ""
	for range maxFindPages {
		resp, err := authenticated.API.ListProjectDeployments(ctx, authenticated.ProjectID, api.DeploymentFilter{}, cursor, pageLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list deployments: %w", err)
		}
		for i := range resp.Data {
			if resp.Data[i].Id == deploymentID {
				return &resp.Data[i], nil
			}
		}
		if !resp.HasMore || resp.NextCursor == nil || len(resp.Data) == 0 {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, deploymentID)
		}
		cursor = *resp.NextCursor
	}
	return nil, fmt.Errorf("%w among the project's newest %d deployments: %s", ErrNotFound, maxFindPages*pageLimit, deploymentID)
}

// Summary counts the outcomes of deployments matching filter.
func (s Service) Summary(ctx context.Context, filter api.DeploymentFilter) (*apiclient.ProjectDeploymentSummary, error) {
	authenticated, err := s.current()
	if err != nil {
		return nil, err
	}
	summary, err := authenticated.API.SummarizeProjectDeployments(ctx, authenticated.ProjectID, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to summarize deployments: %w", err)
	}
	return summary, nil
}
