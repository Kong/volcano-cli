package api

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// AccessTokenCreateInput contains one project access token mint request.
type AccessTokenCreateInput struct {
	Name      string
	Scope     string
	ExpiresAt *time.Time
}

// AccessTokenListInput contains one project access token page request.
type AccessTokenListInput struct {
	Page           int
	Limit          int
	Search         string
	IncludeRevoked bool
}

// ListAccessTokens lists one project access token page.
func (c *Client) ListAccessTokens(ctx context.Context, projectID uuid.UUID, input AccessTokenListInput) (*apiclient.PaginatedProjectAccessTokens, error) {
	params := &apiclient.ListProjectAccessTokensParams{
		Page:  &input.Page,
		Limit: &input.Limit,
	}
	if search := strings.TrimSpace(input.Search); search != "" {
		params.Search = &search
	}
	if input.IncludeRevoked {
		params.IncludeRevoked = &input.IncludeRevoked
	}

	resp, err := c.client.ListProjectAccessTokensWithResponse(ctx, projectID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404)
}

// CreateAccessToken mints one project access token. The response carries the
// plaintext secret, which the API returns only here.
func (c *Client) CreateAccessToken(ctx context.Context, projectID uuid.UUID, input AccessTokenCreateInput) (*apiclient.CreatedProjectAccessToken, error) {
	body := apiclient.CreateProjectAccessTokenJSONRequestBody{
		Name:      strings.TrimSpace(input.Name),
		Scope:     apiclient.ProjectAccessTokenScope(strings.TrimSpace(input.Scope)),
		ExpiresAt: input.ExpiresAt,
	}

	resp, err := c.client.CreateProjectAccessTokenWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	// Never the body: a created token carries its plaintext secret.
	return apiResultWithoutBody(resp.StatusCode(), resp.JSON201, resp.JSON403, resp.JSON404, resp.JSON409)
}

// GetAccessToken returns one project access token by ID.
func (c *Client) GetAccessToken(ctx context.Context, projectID, tokenID uuid.UUID) (*apiclient.ProjectAccessToken, error) {
	resp, err := c.client.GetProjectAccessTokenWithResponse(ctx, projectID, tokenID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404)
}

// GetAccessTokenUsage returns the daily request counts for one project access token.
func (c *Client) GetAccessTokenUsage(ctx context.Context, projectID, tokenID uuid.UUID, days int) (*apiclient.ProjectAccessTokenUsage, error) {
	params := &apiclient.GetProjectAccessTokenUsageParams{}
	if days > 0 {
		params.Days = &days
	}

	resp, err := c.client.GetProjectAccessTokenUsageWithResponse(ctx, projectID, tokenID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404)
}

// ListAccessTokensUsage returns the daily request counts for every access token
// in a project, revoked tokens included.
func (c *Client) ListAccessTokensUsage(ctx context.Context, projectID uuid.UUID, days int) ([]apiclient.ProjectAccessTokenUsage, error) {
	params := &apiclient.ListProjectAccessTokensUsageParams{}
	if days > 0 {
		params.Days = &days
	}

	resp, err := c.client.ListProjectAccessTokensUsageWithResponse(ctx, projectID, params)
	if err != nil {
		return nil, err
	}
	usage, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404)
	if err != nil {
		return nil, err
	}
	return *usage, nil
}

// RevokeAccessToken revokes one project access token by ID.
func (c *Client) RevokeAccessToken(ctx context.Context, projectID, tokenID uuid.UUID) error {
	resp, err := c.client.RevokeProjectAccessTokenWithResponse(ctx, projectID, tokenID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON403, resp.JSON404, resp.JSON409)
}
