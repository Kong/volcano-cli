package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// ProjectConfigValidationError is returned when the server rejects a config
// manifest with 422. Nothing was applied; Errors carries the full list.
type ProjectConfigValidationError struct {
	Errors []apiclient.ProjectConfigValidationError
}

func (e *ProjectConfigValidationError) Error() string {
	return fmt.Sprintf("configuration validation failed with %d error(s); nothing was applied", len(e.Errors))
}

// ApplyProjectConfig uploads a full configuration manifest (pre-encoded JSON)
// and returns the server's per-resource apply report.
func (c *Client) ApplyProjectConfig(ctx context.Context, projectID uuid.UUID, manifestJSON []byte, dryRun bool) (*apiclient.ProjectConfigApplyResult, error) {
	params := &apiclient.ApplyProjectConfigParams{}
	if dryRun {
		params.DryRun = &dryRun
	}
	resp, err := c.client.ApplyProjectConfigWithBodyWithResponse(ctx, projectID, params, "application/json", bytes.NewReader(manifestJSON))
	if err != nil {
		return nil, err
	}
	if resp.JSON422 != nil {
		return nil, &ProjectConfigValidationError{Errors: resp.JSON422.Errors}
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON404, resp.JSON409)
}

// GetProjectConfigYAML downloads the project configuration as the canonical
// YAML manifest rendered by the server, returned verbatim.
func (c *Client) GetProjectConfigYAML(ctx context.Context, projectID uuid.UUID) ([]byte, error) {
	format := apiclient.Yaml
	resp, err := c.client.GetProjectConfig(ctx, projectID, &apiclient.GetProjectConfigParams{Format: &format})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp.StatusCode, body)
	}
	return body, nil
}
