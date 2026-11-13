package api

import (
	"context"
	"encoding/json"

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
	return gitConnection(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// ConnectProjectGit binds a project to a repository. It is a full replace: it
// only binds, and never creates or deletes anything on the git provider.
func (c *Client) ConnectProjectGit(ctx context.Context, projectID uuid.UUID, body apiclient.ConnectProjectGitJSONRequestBody) (*apiclient.ProjectGitConnection, error) {
	resp, err := c.client.ConnectProjectGitWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return gitConnection(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON503)
}

// SetProjectGitRootDirectory changes only the directory a connected project
// builds from, leaving the repository and production branch alone. Unlike
// ConnectProjectGit it accepts a project access token.
func (c *Client) SetProjectGitRootDirectory(ctx context.Context, projectID uuid.UUID, rootDirectory string) (*apiclient.ProjectGitConnection, error) {
	resp, err := c.client.SetProjectGitRootDirectoryWithResponse(ctx, projectID,
		apiclient.SetProjectGitRootDirectoryJSONRequestBody{RootDirectory: rootDirectory})
	if err != nil {
		return nil, err
	}
	return gitConnection(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

// SetProjectGitProductionBranch changes only the branch a connected project
// deploys from. A branch pins the project to it; an empty one makes it follow
// the repository's default branch again. It accepts a project access token.
func (c *Client) SetProjectGitProductionBranch(ctx context.Context, projectID uuid.UUID, branch string) (*apiclient.ProjectGitConnection, error) {
	resp, err := c.client.SetProjectGitProductionBranchWithResponse(ctx, projectID,
		apiclient.SetProjectGitProductionBranchJSONRequestBody{ProductionBranch: branch})
	if err != nil {
		return nil, err
	}
	return gitConnection(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON429, resp.JSON500)
}

// gitConnection is apiResult for a project's repo connection. A server that
// predates production_branch_follows_default leaves it out, and decoding that
// as false would call the branch pinned. Such a server records the branch from
// the repository's default, so a missing field follows it.
func gitConnection(statusCode int, body []byte, result *apiclient.ProjectGitConnection,
	generatedErrors ...*apiclient.Error,
) (*apiclient.ProjectGitConnection, error) {
	connection, err := apiResult(statusCode, body, result, generatedErrors...)
	if err != nil {
		return nil, err
	}
	var fields struct {
		FollowsDefault *bool `json:"production_branch_follows_default"`
	}
	if json.Unmarshal(body, &fields) == nil && fields.FollowsDefault == nil {
		connection.ProductionBranchFollowsDefault = true
	}
	return connection, nil
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

// UpdateProjectGitDeploySettings replaces a project's Git deploy settings:
// what a push to the connected repo's production branch deploys. It is a full
// replace, so a caller changing one setting sends the others back as read.
func (c *Client) UpdateProjectGitDeploySettings(
	ctx context.Context, projectID uuid.UUID, settings apiclient.UpdateProjectGitDeploySettingsRequest,
) (*apiclient.ProjectGitDeploySettings, error) {
	resp, err := c.client.UpdateProjectGitDeploySettingsWithResponse(ctx, projectID, settings)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}
