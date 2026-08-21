package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ListGitConnections returns the caller's stored git provider connections.
func (c *Client) ListGitConnections(ctx context.Context) ([]apiclient.GitConnection, error) {
	resp, err := c.client.ListGitConnectionsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	list, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON500, resp.JSON503)
	if err != nil {
		return nil, err
	}
	return list.Connections, nil
}

// ListGitInstallations returns the GitHub App installations a connection can
// reach. This is a live proxy to GitHub; nothing is persisted by the call.
func (c *Client) ListGitInstallations(ctx context.Context, connectionID uuid.UUID) ([]apiclient.GitInstallation, error) {
	resp, err := c.client.ListGitInstallationsWithResponse(ctx, connectionID)
	if err != nil {
		return nil, err
	}
	list, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON404, resp.JSON500, resp.JSON503)
	if err != nil {
		return nil, err
	}
	return list.Installations, nil
}

// ListGitInstallationRepositories returns the repos reachable through one
// installation.
func (c *Client) ListGitInstallationRepositories(ctx context.Context, connectionID uuid.UUID, installationID int64) ([]apiclient.GitRepository, error) {
	resp, err := c.client.ListGitInstallationRepositoriesWithResponse(ctx, connectionID, installationID)
	if err != nil {
		return nil, err
	}
	list, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON404, resp.JSON500, resp.JSON503)
	if err != nil {
		return nil, err
	}
	return list.Repositories, nil
}

// GetProjectGitConnection returns a project's repo connection. A project with
// no connection answers 404, which surfaces as an *Error carrying that status.
func (c *Client) GetProjectGitConnection(ctx context.Context, projectID uuid.UUID) (*apiclient.ProjectGitConnection, error) {
	resp, err := c.client.GetProjectGitConnectionWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// ConnectProjectGit binds a project to a repository. It is a full replace: it
// only binds, and never creates or deletes anything on the git provider.
func (c *Client) ConnectProjectGit(ctx context.Context, projectID uuid.UUID, body apiclient.ConnectProjectGitJSONRequestBody) (*apiclient.ProjectGitConnection, error) {
	resp, err := c.client.ConnectProjectGitWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON503)
}

// CreateProjectGitRepository creates a new, empty repository on the provider and
// binds the project to it in one call.
//
// Unlike every other call in this file it changes something outside Volcano, and
// something Volcano cannot undo. A response naming a repository reports one that
// exists on GitHub even when the status is a failure, so callers must report the
// name rather than only that the request failed — a caller told only "failed"
// creates a second repository.
func (c *Client) CreateProjectGitRepository(
	ctx context.Context, projectID uuid.UUID, body apiclient.CreateProjectGitRepositoryJSONRequestBody,
) (*apiclient.CreatedProjectGitConnection, error) {
	resp, err := c.client.CreateProjectGitRepositoryWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body,
		resp.JSON201, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404,
		resp.JSON409, resp.JSON422, resp.JSON429, resp.JSON500, resp.JSON503)
}

// SetProjectGitProductionBranch points a bound project at the branch a push must
// land on to deploy.
//
// Separate from the bind because the bind refuses to change repository and name a
// non-default branch in one request: the branch named there is almost always the
// previous repository's, echoed back. Connect first, then set the branch.
func (c *Client) SetProjectGitProductionBranch(
	ctx context.Context, projectID uuid.UUID, body apiclient.SetProjectGitProductionBranchJSONRequestBody,
) (*apiclient.ProjectGitConnection, error) {
	resp, err := c.client.SetProjectGitProductionBranchWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body,
		resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// DisconnectProjectGit removes a project's repo connection. The repository
// itself is untouched.
func (c *Client) DisconnectProjectGit(ctx context.Context, projectID uuid.UUID) error {
	resp, err := c.client.DisconnectProjectGitWithResponse(ctx, projectID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// GetProjectGitDeploySettings returns what a push to the connected repo's
// production branch deploys.
func (c *Client) GetProjectGitDeploySettings(ctx context.Context, projectID uuid.UUID) (*apiclient.ProjectGitDeploySettings, error) {
	resp, err := c.client.GetProjectGitDeploySettingsWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}
