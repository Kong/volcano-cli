package api

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/archive"
)

// FrontendDeployInput contains one packaged frontend source archive.
type FrontendDeployInput struct {
	Name          string
	Framework     string
	AppRoot       string
	VariableScope string
	Variables     []string
	Archive       []byte
}

// FrontendCustomDomainInput contains one custom domain attach request. An
// empty TLSMode means BYOC. A managed request carries no PEM material.
type FrontendCustomDomainInput struct {
	Domain              string
	TLSMode             apiclient.FrontendCustomDomainTLSConfigMode
	CertificatePEM      string
	PrivateKeyPEM       string
	CertificateChainPEM string
}

// FrontendDomainInput adds one custom domain beside a frontend's others.
// Without a certificate, Volcano issues one.
type FrontendDomainInput struct {
	Domain              string
	CertificatePEM      string
	PrivateKeyPEM       string
	CertificateChainPEM string
	Redirect            *FrontendDomainRedirect
}

// FrontendDomainRedirect sends every request for a domain to another domain of
// the same frontend. A zero Status takes the server default, 308.
type FrontendDomainRedirect struct {
	To     string
	Status int
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

// GetFrontendLogs returns one runtime log search page for a frontend.
func (c *Client) GetFrontendLogs(ctx context.Context, projectID, frontendID uuid.UUID, limit int, cursor string) (*apiclient.LogSearchResponse, error) {
	body := logSearchRequest{
		Resource: logResource(logResourceTypeFrontend, frontendID),
	}
	if limit > 0 {
		body.Limit = &limit
	}
	if cursor != "" {
		body.Cursor = &cursor
	}
	return c.searchProjectLogs(ctx, projectID, body)
}

// StreamFrontendLogs opens a runtime log stream for a frontend.
func (c *Client) StreamFrontendLogs(ctx context.Context, projectID, frontendID uuid.UUID, limit int, lastEventID string) (*ProjectLogStream, error) {
	body := logSearchRequest{
		Resource: logResource(logResourceTypeFrontend, frontendID),
	}
	if limit > 0 {
		body.Limit = &limit
	}
	return c.streamProjectLogs(ctx, projectID, body, lastEventID)
}

// GetFrontendDeploymentLogs returns one build log search page for a frontend deployment.
func (c *Client) GetFrontendDeploymentLogs(ctx context.Context, projectID, frontendID, deploymentID uuid.UUID, limit int, cursor string) (*apiclient.LogSearchResponse, error) {
	body := logSearchRequest{
		Resource: logDeploymentResource(logResourceTypeFrontend, frontendID, deploymentID),
	}
	if limit > 0 {
		body.Limit = &limit
	}
	if cursor != "" {
		body.Cursor = &cursor
	}
	return c.searchProjectLogs(ctx, projectID, body)
}

// StreamFrontendDeploymentLogs opens a build log stream for a frontend deployment.
func (c *Client) StreamFrontendDeploymentLogs(ctx context.Context, projectID, frontendID, deploymentID uuid.UUID, limit int, lastEventID string) (*ProjectLogStream, error) {
	body := logSearchRequest{
		Resource: logDeploymentResource(logResourceTypeFrontend, frontendID, deploymentID),
	}
	if limit > 0 {
		body.Limit = &limit
	}
	return c.streamProjectLogs(ctx, projectID, body, lastEventID)
}

// CreateFrontendCustomDomain attaches a custom domain to a frontend. A request
// for the hostname of the frontend's active domain in the other TLS mode
// switches that domain in place. created is false when the server answered for
// a domain the frontend already had.
func (c *Client) CreateFrontendCustomDomain(ctx context.Context, projectID, frontendID uuid.UUID, input FrontendCustomDomainInput) (domain *apiclient.FrontendCustomDomainResponse, created bool, err error) {
	body := apiclient.CreateFrontendCustomDomainJSONRequestBody{
		Domain: strings.TrimSpace(input.Domain),
		Tls:    frontendCustomDomainTLSConfig(input),
	}
	resp, err := c.client.CreateFrontendCustomDomainWithResponse(ctx, projectID, frontendID, body)
	if err != nil {
		return nil, false, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, true, nil
	}
	if resp.JSON200 != nil {
		return resp.JSON200, false, nil
	}
	if resp.JSON409 != nil {
		return nil, false, conflictError(resp.StatusCode(), resp.JSON409)
	}
	return nil, false, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON503)
}

func frontendCustomDomainTLSConfig(input FrontendCustomDomainInput) apiclient.FrontendCustomDomainTLSConfig {
	if input.TLSMode == apiclient.FrontendCustomDomainTLSConfigModeManaged {
		return apiclient.FrontendCustomDomainTLSConfig{Mode: apiclient.FrontendCustomDomainTLSConfigModeManaged}
	}
	certificate := strings.TrimSpace(input.CertificatePEM)
	privateKey := strings.TrimSpace(input.PrivateKeyPEM)
	config := apiclient.FrontendCustomDomainTLSConfig{
		CertificatePem: &certificate,
		Mode:           apiclient.FrontendCustomDomainTLSConfigModeByoc,
		PrivateKeyPem:  &privateKey,
	}
	if chain := strings.TrimSpace(input.CertificateChainPEM); chain != "" {
		config.CertificateChainPem = &chain
	}
	return config
}

// GetFrontendCustomDomain returns the configured custom domain for a frontend.
// It returns ErrNotFound when the frontend has none.
func (c *Client) GetFrontendCustomDomain(ctx context.Context, projectID, frontendID uuid.UUID) (*apiclient.FrontendCustomDomainResponse, error) {
	resp, err := c.client.GetFrontendCustomDomainWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return nil, err
	}
	// The server answers 200 with a JSON null for a frontend without a domain,
	// which the generated parser decodes into an empty response.
	if resp.JSON200 != nil && bytes.Equal(bytes.TrimSpace(resp.Body), []byte("null")) {
		return nil, fmt.Errorf("custom domain %w", ErrNotFound)
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

// ListFrontendDomains lists a frontend's custom domains, oldest first.
func (c *Client) ListFrontendDomains(ctx context.Context, projectID, frontendID uuid.UUID) ([]apiclient.FrontendCustomDomainResponse, error) {
	resp, err := c.client.ListFrontendDomainsWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return nil, err
	}
	list, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
	if err != nil {
		return nil, err
	}
	return list.Data, nil
}

// CreateFrontendDomain adds a custom domain to a frontend. It reports false
// when the frontend already had the hostname, whose TLS mode the request may
// have switched in place.
func (c *Client) CreateFrontendDomain(ctx context.Context, projectID, frontendID uuid.UUID, input FrontendDomainInput) (*apiclient.FrontendCustomDomainResponse, bool, error) {
	body := apiclient.CreateFrontendDomainJSONRequestBody{
		Domain:   strings.TrimSpace(input.Domain),
		Tls:      frontendDomainTLS(input),
		Redirect: frontendDomainRedirectRequest(input.Redirect),
	}
	resp, err := c.client.CreateFrontendDomainWithResponse(ctx, projectID, frontendID, body)
	if err != nil {
		return nil, false, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, true, nil
	}
	if resp.JSON200 != nil {
		return resp.JSON200, false, nil
	}
	if resp.JSON409 != nil {
		return nil, false, conflictError(resp.StatusCode(), resp.JSON409)
	}
	return nil, false, apiErrorFromGeneratedErrors(resp.StatusCode(), resp.Body, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON503)
}

// GetFrontendDomain returns one custom domain of a frontend.
func (c *Client) GetFrontendDomain(ctx context.Context, projectID, frontendID uuid.UUID, domain string) (*apiclient.FrontendCustomDomainResponse, error) {
	resp, err := c.client.GetFrontendDomainWithResponse(ctx, projectID, frontendID, domain)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
}

// SetFrontendDomainRedirect makes a custom domain redirect, or serve the
// frontend again when redirect is nil.
func (c *Client) SetFrontendDomainRedirect(
	ctx context.Context, projectID, frontendID uuid.UUID, domain string, redirect *FrontendDomainRedirect,
) (*apiclient.FrontendCustomDomainResponse, error) {
	body := apiclient.UpdateFrontendDomainJSONRequestBody{Redirect: frontendDomainRedirectRequest(redirect)}
	resp, err := c.client.UpdateFrontendDomainWithResponse(ctx, projectID, frontendID, domain, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

// DeleteFrontendDomain removes one custom domain from a frontend.
func (c *Client) DeleteFrontendDomain(ctx context.Context, projectID, frontendID uuid.UUID, domain string) error {
	resp, err := c.client.DeleteFrontendDomainWithResponse(ctx, projectID, frontendID, domain)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

func frontendDomainTLS(input FrontendDomainInput) apiclient.FrontendCustomDomainTLSConfig {
	certificate := strings.TrimSpace(input.CertificatePEM)
	if certificate == "" {
		return apiclient.FrontendCustomDomainTLSConfig{Mode: apiclient.FrontendCustomDomainTLSConfigModeManaged}
	}
	privateKey := strings.TrimSpace(input.PrivateKeyPEM)
	tls := apiclient.FrontendCustomDomainTLSConfig{
		Mode:           apiclient.FrontendCustomDomainTLSConfigModeByoc,
		CertificatePem: &certificate,
		PrivateKeyPem:  &privateKey,
	}
	if chain := strings.TrimSpace(input.CertificateChainPEM); chain != "" {
		tls.CertificateChainPem = &chain
	}
	return tls
}

func frontendDomainRedirectRequest(redirect *FrontendDomainRedirect) *apiclient.FrontendCustomDomainRedirectRequest {
	if redirect == nil {
		return nil
	}
	request := &apiclient.FrontendCustomDomainRedirectRequest{To: strings.TrimSpace(redirect.To)}
	if redirect.Status != 0 {
		status := apiclient.FrontendCustomDomainRedirectRequestStatus(redirect.Status)
		request.Status = &status
	}
	return request
}

// ListFrontendFunctionRoutes lists the paths a frontend forwards to functions.
func (c *Client) ListFrontendFunctionRoutes(ctx context.Context, projectID, frontendID uuid.UUID) ([]apiclient.FrontendFunctionRoute, error) {
	resp, err := c.client.ListFrontendFunctionRoutesWithResponse(ctx, projectID, frontendID)
	if err != nil {
		return nil, err
	}
	list, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON401, resp.JSON403, resp.JSON500)
	if err != nil {
		return nil, err
	}
	return list.Data, nil
}

// CreateFrontendFunctionRoute forwards a path prefix on a frontend to a function.
func (c *Client) CreateFrontendFunctionRoute(
	ctx context.Context, projectID, frontendID uuid.UUID, route apiclient.CreateFrontendFunctionRouteRequest,
) (*apiclient.FrontendFunctionRoute, error) {
	resp, err := c.client.CreateFrontendFunctionRouteWithResponse(ctx, projectID, frontendID, route)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON201,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

// UpdateFrontendFunctionRoute replaces one route's prefix, target, and stripping.
func (c *Client) UpdateFrontendFunctionRoute(
	ctx context.Context, projectID, frontendID, routeID uuid.UUID, route apiclient.CreateFrontendFunctionRouteRequest,
) (*apiclient.FrontendFunctionRoute, error) {
	resp, err := c.client.UpdateFrontendFunctionRouteWithResponse(ctx, projectID, frontendID, routeID, route)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON500)
}

// DeleteFrontendFunctionRoute stops forwarding one route's path prefix.
func (c *Client) DeleteFrontendFunctionRoute(ctx context.Context, projectID, frontendID, routeID uuid.UUID) error {
	resp, err := c.client.DeleteFrontendFunctionRouteWithResponse(ctx, projectID, frontendID, routeID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500)
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
	if fn.VariableScope != "" {
		if err := writer.WriteField("variable_scope", fn.VariableScope); err != nil {
			return nil, "", fmt.Errorf("failed to write variable_scope field: %w", err)
		}
	}
	for _, variable := range fn.Variables {
		if err := writer.WriteField("variables", variable); err != nil {
			return nil, "", fmt.Errorf("failed to write variables field: %w", err)
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
