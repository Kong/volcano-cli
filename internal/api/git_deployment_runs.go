package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// GitDeploymentRun is one push's deployment through Git auto deploy, as the
// API sent it.
//
// Config is what applying the push's opted-in volcano-config.yaml sections
// did. It is kept as sent because the vendored contract does not describe it
// yet, so --json passes it on whole. It is empty when the API reports none.
type GitDeploymentRun struct {
	apiclient.GitDeploymentRun
	Config json.RawMessage `json:"config,omitempty"`
}

// GitDeploymentRunsPage is one page of a project's Git deployment runs.
type GitDeploymentRunsPage struct {
	Data       []GitDeploymentRun `json:"data"`
	Limit      int                `json:"limit"`
	HasMore    bool               `json:"has_more"`
	NextCursor *string            `json:"next_cursor,omitempty"`
}

// ListGitDeploymentRuns pages a project's Git deployment runs, newest push
// first.
func (c *Client) ListGitDeploymentRuns(
	ctx context.Context, projectID uuid.UUID, params apiclient.ListGitDeploymentRunsParams,
) (*GitDeploymentRunsPage, error) {
	resp, err := c.client.ListGitDeploymentRunsWithResponse(ctx, projectID, &params)
	if err != nil {
		return nil, err
	}
	if _, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404); err != nil {
		return nil, err
	}
	var page GitDeploymentRunsPage
	if err := json.Unmarshal(resp.Body, &page); err != nil {
		return nil, fmt.Errorf("decode Git deployment runs: %w", err)
	}
	if page.Data == nil {
		page.Data = []GitDeploymentRun{}
	}
	return &page, nil
}

// GetGitDeploymentRun returns one of a project's Git deployment runs.
func (c *Client) GetGitDeploymentRun(ctx context.Context, projectID, runID uuid.UUID) (*GitDeploymentRun, error) {
	resp, err := c.client.GetGitDeploymentRunWithResponse(ctx, projectID, runID)
	if err != nil {
		return nil, err
	}
	if _, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON401, resp.JSON403, resp.JSON404); err != nil {
		return nil, err
	}
	var run GitDeploymentRun
	if err := json.Unmarshal(resp.Body, &run); err != nil {
		return nil, fmt.Errorf("decode Git deployment run: %w", err)
	}
	return &run, nil
}
