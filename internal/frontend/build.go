package frontend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	clisession "github.com/Kong/volcano-cli/internal/session"
)

// BuildSource is the value of "frontends deploy --from": a frontend and,
// optionally, one of its deployments.
type BuildSource struct {
	Frontend     string
	DeploymentID string
}

// ParseBuildSource reads <frontend>[@<deployment-id>].
func ParseBuildSource(value string) (BuildSource, error) {
	name, deploymentID, hasDeployment := strings.Cut(strings.TrimSpace(value), "@")
	name = strings.TrimSpace(name)
	deploymentID = strings.TrimSpace(deploymentID)
	if name == "" || (hasDeployment && deploymentID == "") {
		return BuildSource{}, fmt.Errorf("--from %q must be <frontend> or <frontend>@<deployment-id>", value)
	}
	if hasDeployment && uuid.Validate(deploymentID) != nil {
		return BuildSource{}, fmt.Errorf("--from %q: %q is not a deployment ID", value, deploymentID)
	}
	return BuildSource{Frontend: name, DeploymentID: deploymentID}, nil
}

// DeployedBuild is a deployment started from another frontend's build.
type DeployedBuild struct {
	Frontend         *apiclient.Frontend
	Source           *apiclient.Frontend
	SourceDeployment *apiclient.FrontendDeployment
}

// DeployBuild deploys the build of a source frontend's deployment to target
// without compiling it again. Without a deployment ID it uses the source's
// newest active or degraded deployment, which is the one it serves.
func (s Service) DeployBuild(ctx context.Context, target string, source BuildSource) (*DeployedBuild, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}
	targetFrontend, err := resolveNamedFrontend(ctx, authenticated, target)
	if err != nil {
		return nil, err
	}
	sourceFrontend, err := resolveNamedFrontend(ctx, authenticated, source.Frontend)
	if err != nil {
		return nil, err
	}
	deployment, err := buildDeployment(ctx, authenticated, sourceFrontend, source.DeploymentID)
	if err != nil {
		return nil, err
	}

	deployed, err := authenticated.API.CreateFrontendDeployment(ctx, authenticated.ProjectID, targetFrontend.Id, deployment.Id)
	if err != nil {
		return nil, fmt.Errorf("failed to deploy the build of %s@%s to frontend %q: %w",
			sourceFrontend.Name, deployment.Id, targetFrontend.Name, err)
	}
	if deployed.Name == "" {
		deployed.Name = targetFrontend.Name
	}
	return &DeployedBuild{Frontend: deployed, Source: sourceFrontend, SourceDeployment: deployment}, nil
}

func resolveNamedFrontend(ctx context.Context, authenticated *clisession.ProjectSession, identifier string) (*apiclient.Frontend, error) {
	frontend, err := resolveFrontend(ctx, authenticated, identifier)
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("frontend %q %w", identifier, api.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve frontend %q: %w", identifier, err)
	}
	return frontend, nil
}

func buildDeployment(
	ctx context.Context,
	authenticated *clisession.ProjectSession,
	source *apiclient.Frontend,
	deploymentID string,
) (*apiclient.FrontendDeployment, error) {
	if deploymentID != "" {
		deployment, err := findDeployment(ctx, authenticated, source.Id, func(d *apiclient.FrontendDeployment) bool {
			return d.Id.String() == deploymentID
		})
		if errors.Is(err, api.ErrNotFound) {
			return nil, fmt.Errorf("frontend %q has no deployment %s", source.Name, deploymentID)
		}
		return deployment, err
	}
	deployment, err := findDeployment(ctx, authenticated, source.Id, func(d *apiclient.FrontendDeployment) bool {
		return d.Status == apiclient.FrontendDeploymentStatusActive || d.Status == apiclient.FrontendDeploymentStatusDegraded
	})
	if errors.Is(err, api.ErrNotFound) {
		return nil, fmt.Errorf("frontend %q has no active or degraded deployment to deploy from", source.Name)
	}
	return deployment, err
}
