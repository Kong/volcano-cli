package api

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
	"github.com/Kong/volcano-cli/internal/archive"
)

// FrontendDeployInput contains one packaged frontend source archive.
type FrontendDeployInput struct {
	Name      string
	Framework string
	AppRoot   string
	Archive   []byte
}

// FrontendCustomDomainInput contains one custom domain attach request.
type FrontendCustomDomainInput struct {
	Domain              string
	CertificatePEM      string
	PrivateKeyPEM       string
	CertificateChainPEM string
}

// ListFrontends lists one frontend page for a project.
func (c *Client) ListFrontends(ctx context.Context, projectID uuid.UUID, page, limit int) (*apiclient.PaginatedFrontends, error) {
	resp, err := c.client.ListFrontendsWithResponse(ctx, projectID, &apiclient.ListFrontendsParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// DeployFrontend uploads one frontend source archive.
func (c *Client) DeployFrontend(ctx context.Context, projectID uuid.UUID, input FrontendDeployInput) (*apiclient.Frontend, error) {
	body, contentType, err := buildFrontendDeployMultipart(input)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.CreateFrontendWithBodyWithResponse(ctx, projectID, contentType, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, nil
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	status := resp.StatusCode()
	if status >= 200 && status < 300 {
		// Contract violation: the server accepted the deploy but returned an
		// empty body. Treat as success and synthesize a minimal frontend so
		// the CLI does not report a successful deploy as a failure.
		return &apiclient.Frontend{Name: input.Name}, nil
	}
	return nil, apiErrorFromGeneratedErrors(status, resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500, resp.JSON503)
}

// GetFrontend returns one frontend by ID.
func (c *Client) GetFrontend(ctx context.Context, projectID, frontendID uuid.UUID) (*apiclient.Frontend, error) {
	resp, err := c.client.GetFrontendWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// DeleteFrontend starts deleting one frontend by ID.
func (c *Client) DeleteFrontend(ctx context.Context, projectID, frontendID uuid.UUID) error {
	resp, err := c.client.DeleteFrontendWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON503)
}

// RedeployFrontend starts a new deployment using the last uploaded archive.
func (c *Client) RedeployFrontend(ctx context.Context, projectID, frontendID uuid.UUID) (*apiclient.Frontend, error) {
	resp, err := c.client.RedeployFrontendWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return nil, err
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	status := resp.StatusCode()
	if status >= 200 && status < 300 {
		// Contract violation: the server accepted the redeploy but returned
		// an empty body. Treat as success and synthesize a minimal frontend
		// so the CLI does not report a successful redeploy as a failure.
		return &apiclient.Frontend{Id: frontendID}, nil
	}
	return nil, apiErrorFromGeneratedErrors(status, resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500, resp.JSON503)
}

// ListFrontendDeployments lists one deployment page for a frontend.
func (c *Client) ListFrontendDeployments(ctx context.Context, projectID, frontendID uuid.UUID, page, limit int) (*apiclient.PaginatedFrontendDeployments, error) {
	resp, err := c.client.ListFrontendDeploymentsWithResponse(ctx, projectID, frontendID, &apiclient.ListFrontendDeploymentsParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// GetFrontendLogs returns one runtime log page for a frontend.
func (c *Client) GetFrontendLogs(ctx context.Context, projectID, frontendID uuid.UUID, limit int, cursor string) (*apiclient.ListLogsResponse, error) {
	params := &apiclient.GetFrontendLogsParams{}
	if limit > 0 {
		params.Limit = &limit
	}
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		params.Cursor = &cursor
	}

	resp, err := c.client.GetFrontendLogsWithResponse(ctx, projectID, frontendID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON503)
}

// GetFrontendDeploymentLogs returns one build log page for a frontend deployment.
func (c *Client) GetFrontendDeploymentLogs(ctx context.Context, projectID, frontendID, deploymentID uuid.UUID, limit int, cursor string) (*apiclient.ListLogsResponse, error) {
	params := &apiclient.GetFrontendDeploymentLogsParams{}
	if limit > 0 {
		params.Limit = &limit
	}
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		params.Cursor = &cursor
	}

	resp, err := c.client.GetFrontendDeploymentLogsWithResponse(ctx, projectID, frontendID, deploymentID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON503)
}

// CreateFrontendCustomDomain attaches a BYOC custom domain to a frontend.
func (c *Client) CreateFrontendCustomDomain(ctx context.Context, projectID, frontendID uuid.UUID, input FrontendCustomDomainInput) (*apiclient.FrontendCustomDomainResponse, error) {
	body := apiclient.CreateFrontendCustomDomainJSONRequestBody{
		Domain: strings.TrimSpace(input.Domain),
		Tls: apiclient.FrontendCustomDomainTLSConfig{
			CertificatePem: strings.TrimSpace(input.CertificatePEM),
			Mode:           apicommon.FrontendCustomDomainTLSConfigModeByoc,
			PrivateKeyPem:  strings.TrimSpace(input.PrivateKeyPEM),
		},
	}
	if chain := strings.TrimSpace(input.CertificateChainPEM); chain != "" {
		body.Tls.CertificateChainPem = &chain
	}

	resp, err := c.client.CreateFrontendCustomDomainWithResponse(ctx, projectID, frontendID, body)
	if err != nil {
		return nil, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, nil
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	return nil, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500, resp.JSON503)
}

// GetFrontendCustomDomain returns the configured custom domain for a frontend.
func (c *Client) GetFrontendCustomDomain(ctx context.Context, projectID, frontendID uuid.UUID) (*apiclient.FrontendCustomDomainResponse, error) {
	resp, err := c.client.GetFrontendCustomDomainWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// DeleteFrontendCustomDomain detaches the configured custom domain from a frontend.
func (c *Client) DeleteFrontendCustomDomain(ctx context.Context, projectID, frontendID uuid.UUID) error {
	resp, err := c.client.DeleteFrontendCustomDomainWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

func buildFrontendDeployMultipart(fn FrontendDeployInput) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("name", fn.Name); err != nil {
		return nil, "", fmt.Errorf("failed to write name field: %w", err)
	}
	if fn.Framework != "" {
		if err := writer.WriteField("framework", fn.Framework); err != nil {
			return nil, "", fmt.Errorf("failed to write framework field: %w", err)
		}
	}
	if fn.AppRoot != "" {
		if err := writer.WriteField("app_root", fn.AppRoot); err != nil {
			return nil, "", fmt.Errorf("failed to write app_root field: %w", err)
		}
	}
	if err := archive.WriteArchivePart(writer, "archive", fn.Name, fn.Archive); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to finalize multipart body: %w", err)
	}
	return body, writer.FormDataContentType(), nil
}
