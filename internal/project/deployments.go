package project

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

// deploymentPageLimit is the API's page cap.
const deploymentPageLimit = 100

// maxDeploymentPages bounds one listing; a project deploy starts at most a few
// hundred deployments.
const maxDeploymentPages = 20

// Target is the project a command acts on.
type Target struct {
	ID uuid.UUID
	// Name is empty when the project could not be read by name.
	Name string
}

// CurrentTarget resolves the current project, and its name when the API or the
// saved selection has one. A missing name is not an error.
func (s Service) CurrentTarget(ctx context.Context) (Target, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return Target{}, err
	}
	target := Target{ID: authenticated.ProjectID}
	if project, err := authenticated.API.GetProject(ctx, authenticated.ProjectID); err == nil {
		target.Name = project.Name
	} else if saved := authenticated.Config.CurrentProject; saved != nil && saved.ID == authenticated.ProjectID.String() {
		target.Name = saved.Name
	}
	return target, nil
}

// SourceMode reports whether the current project deploys from Volcano
// (platform) or from its connected repository. A server without source export
// has no repository mode, so it reports platform.
func (s Service) SourceMode(ctx context.Context) (apiclient.ProjectSourceExportStateMode, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return "", err
	}
	state, err := authenticated.API.GetProjectSourceExport(ctx, authenticated.ProjectID)
	if api.Status(err) == http.StatusNotImplemented {
		return apiclient.ProjectSourceExportStateModePlatform, nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to read the project's deploy source: %w", err)
	}
	return state.Mode, nil
}

// DeploymentsSince lists the current project's function and frontend
// deployments created at or after since.
func (s Service) DeploymentsSince(ctx context.Context, since time.Time) ([]apiclient.ProjectDeployment, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	var deployments []apiclient.ProjectDeployment
	for page := 1; page <= maxDeploymentPages; page++ {
		resp, err := authenticated.API.ListProjectDeploymentsSince(ctx, authenticated.ProjectID, since, page, deploymentPageLimit)
		if err != nil {
			return nil, fmt.Errorf("failed to list deployments: %w", err)
		}
		deployments = append(deployments, resp.Data...)
		if !resp.HasMore || len(resp.Data) == 0 {
			break
		}
	}
	return deployments, nil
}
