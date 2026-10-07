package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
)

// SandboxDeployInput contains a custom image build context and configuration.
type SandboxDeployInput struct {
	Name          string
	MemoryMB      int
	Ports         []int
	SourceArchive []byte
}

// DeploySandbox uploads a build context for a stable template ID.
func (c *Client) DeploySandbox(ctx context.Context, project, template, key uuid.UUID, input SandboxDeployInput) (*apiclient.SandboxDeployment, error) {
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	ports, err := json.Marshal(input.Ports)
	if err != nil {
		return nil, err
	}
	if input.Ports == nil {
		ports = []byte("[]")
	}
	for _, field := range []struct{ name, value string }{{"name", input.Name}, {"memory_mb", strconv.Itoa(input.MemoryMB)}, {"ports", string(ports)}} {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return nil, err
		}
	}
	if err := archive.WriteArchivePart(writer, "code", input.Name, input.SourceArchive); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	response, err := c.client.DeploySandboxWithBodyWithResponse(ctx, project, template, &apiclient.DeploySandboxParams{IdempotencyKey: key}, writer.FormDataContentType(), body)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON202, response.JSONDefault)
}

// SandboxDeployments lists one deployment history page.
func (c *Client) SandboxDeployments(ctx context.Context, project, template uuid.UUID, cursor string) (*apiclient.SandboxDeploymentPage, error) {
	params := &apiclient.ListSandboxDeploymentsParams{}
	if cursor != "" {
		params.Cursor = &cursor
	}
	response, err := c.client.ListSandboxDeploymentsWithResponse(ctx, project, template, params)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// SandboxDeployment reads the current build and validation status.
func (c *Client) SandboxDeployment(ctx context.Context, project, template, deployment uuid.UUID) (*apiclient.SandboxDeployment, error) {
	response, err := c.client.GetSandboxDeploymentWithResponse(ctx, project, template, deployment)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}

// SandboxDeploymentSource streams the original build context to output.
func (c *Client) SandboxDeploymentSource(ctx context.Context, project, template, deployment uuid.UUID, output io.Writer) error {
	response, err := c.client.GetSandboxDeploymentSource(ctx, project, template, deployment)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		parsed, parseErr := apiclient.ParseGetSandboxDeploymentSourceClientResponse(response)
		if parseErr != nil {
			return parseErr
		}
		return apiOK(parsed.StatusCode(), parsed.Body, parsed.JSONDefault)
	}
	_, err = io.Copy(output, response.Body)
	return err
}

// SandboxDeploymentLogs reads a bounded page of regional build logs.
func (c *Client) SandboxDeploymentLogs(ctx context.Context, project, template, deployment uuid.UUID, params *apiclient.GetSandboxDeploymentLogsParams) (*apiclient.SandboxBuildLogPage, error) {
	response, err := c.client.GetSandboxDeploymentLogsWithResponse(ctx, project, template, deployment, params)
	if err != nil {
		return nil, err
	}
	return apiResult(response.StatusCode(), response.Body, response.JSON200, response.JSONDefault)
}
