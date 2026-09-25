package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// SandboxPresets calls the Sandbox ListSandboxPresets operation.
func (c *Client) SandboxPresets(ctx context.Context) (*apiclient.SandboxPresetList, error) {
	response, err := c.client.ListSandboxPresetsWithResponse(ctx)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// SandboxSessions calls the Sandbox ListSandboxSessions operation.
func (c *Client) SandboxSessions(ctx context.Context, project uuid.UUID, params *apiclient.ListSandboxSessionsParams) (*apiclient.SandboxSessionPage, error) {
	response, err := c.client.ListSandboxSessionsWithResponse(ctx, project, params)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// SandboxTemplates calls the Sandbox ListSandboxes operation.
func (c *Client) SandboxTemplates(ctx context.Context, project uuid.UUID, params *apiclient.ListSandboxesParams) (*apiclient.SandboxTemplatePage, error) {
	response, err := c.client.ListSandboxesWithResponse(ctx, project, params)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// SandboxTemplate calls the Sandbox GetSandbox operation.
func (c *Client) SandboxTemplate(ctx context.Context, project, id uuid.UUID) (*apiclient.SandboxTemplate, error) {
	response, err := c.client.GetSandboxWithResponse(ctx, project, id)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// CreateSandboxTemplate calls the Sandbox CreateSandbox operation.
func (c *Client) CreateSandboxTemplate(ctx context.Context, project, key uuid.UUID, body apiclient.CreateSandboxTemplateRequest) (*apiclient.SandboxTemplate, error) {
	response, err := c.client.CreateSandboxWithResponse(ctx, project, &apiclient.CreateSandboxParams{IdempotencyKey: key}, body)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON201, response.JSONDefault)
}

// StartSandbox calls the Sandbox CreateSandboxSession operation.
func (c *Client) StartSandbox(ctx context.Context, project, key uuid.UUID, body apiclient.CreateSandboxSessionRequest) (*apiclient.SandboxSession, error) {
	response, err := c.client.CreateSandboxSessionWithResponse(ctx, project, &apiclient.CreateSandboxSessionParams{IdempotencyKey: key}, body)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON201, response.JSONDefault)
}

// ExecSandbox calls the Sandbox ExecuteSandbox operation.
func (c *Client) ExecSandbox(ctx context.Context, project, key uuid.UUID, body apiclient.SandboxExecutionRequest) (*apiclient.SandboxExecutionResult, error) {
	response, err := c.client.ExecuteSandboxWithResponse(ctx, project, &apiclient.ExecuteSandboxParams{IdempotencyKey: key}, body)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// SandboxSession calls the Sandbox GetSandboxSession operation.
func (c *Client) SandboxSession(ctx context.Context, id uuid.UUID) (*apiclient.SandboxSession, error) {
	response, err := c.client.GetSandboxSessionWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// SuspendSandbox calls the Sandbox SuspendSandboxSession operation.
func (c *Client) SuspendSandbox(ctx context.Context, id uuid.UUID) (*apiclient.SandboxSession, error) {
	response, err := c.client.SuspendSandboxSessionWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON202, response.JSONDefault)
}

// ResumeSandbox calls the Sandbox ResumeSandboxSession operation.
func (c *Client) ResumeSandbox(ctx context.Context, id uuid.UUID) (*apiclient.SandboxSession, error) {
	response, err := c.client.ResumeSandboxSessionWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON202, response.JSONDefault)
}

// TerminateSandbox calls the Sandbox TerminateSandboxSession operation.
func (c *Client) TerminateSandbox(ctx context.Context, id uuid.UUID) (*apiclient.SandboxSession, error) {
	response, err := c.client.TerminateSandboxSessionWithResponse(ctx, id)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON202, response.JSONDefault)
}

// ExecSandboxSession calls the Sandbox ExecuteSandboxSession operation.
func (c *Client) ExecSandboxSession(ctx context.Context, id, key uuid.UUID, body apiclient.SandboxCommandRequest) (*apiclient.SandboxCommandResult, error) {
	response, err := c.client.ExecuteSandboxSessionWithResponse(ctx, id, &apiclient.ExecuteSandboxSessionParams{IdempotencyKey: key}, body)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// ReadSandboxFile calls the Sandbox ReadSandboxSessionFile operation.
func (c *Client) ReadSandboxFile(ctx context.Context, id uuid.UUID, body apiclient.SandboxFileReadRequest) (*apiclient.SandboxFileResult, error) {
	response, err := c.client.ReadSandboxSessionFileWithResponse(ctx, id, body)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// SandboxAccess calls the Sandbox CreateSandboxSessionAccess operation.
func (c *Client) SandboxAccess(ctx context.Context, id uuid.UUID, body apiclient.SandboxAccessRequest) (*apiclient.SandboxAccess, error) {
	response, err := c.client.CreateSandboxSessionAccessWithResponse(ctx, id, body)
	if err != nil {
		return nil, err
	}
	return apiResultWithoutBody(response.StatusCode(), response.JSON200, response.JSONDefault)
}

// WriteSandboxFile calls the Sandbox WriteSandboxSessionFile operation.
func (c *Client) WriteSandboxFile(ctx context.Context, id uuid.UUID, body apiclient.SandboxFileWriteRequest) error {
	response, err := c.client.WriteSandboxSessionFile(ctx, id, body)
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusNoContent {
		return response.Body.Close()
	}
	parsed, err := apiclient.ParseWriteSandboxSessionFileClientResponse(response)
	if err != nil {
		return err
	}
	return apiOK(parsed.StatusCode(), parsed.Body, parsed.JSONDefault)
}

// DeleteSandboxTemplate calls the Sandbox DeleteSandbox operation.
func (c *Client) DeleteSandboxTemplate(ctx context.Context, project, id uuid.UUID) error {
	response, err := c.client.DeleteSandbox(ctx, project, id)
	if err != nil {
		return err
	}
	if response.StatusCode == http.StatusNoContent {
		return response.Body.Close()
	}
	parsed, err := apiclient.ParseDeleteSandboxClientResponse(response)
	if err != nil {
		return err
	}
	return apiOK(parsed.StatusCode(), parsed.Body, parsed.JSONDefault)
}
