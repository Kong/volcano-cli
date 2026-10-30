package api

import (
	"context"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// AccountSessionsPage is one page of the account's sign-in sessions.
type AccountSessionsPage struct {
	Sessions   []apiclient.AuthSession `json:"sessions"`
	Page       int                     `json:"page"`
	Limit      int                     `json:"limit"`
	Total      int                     `json:"total"`
	TotalPages int                     `json:"total_pages"`
}

// RevokeCurrentToken revokes the platform token this client authenticates
// with, and the sign-in session it was created from. The token is refused
// afterwards. A local-mode server has no sign-in to end and answers 501.
func (c *Client) RevokeCurrentToken(ctx context.Context) error {
	resp, err := c.client.RevokeCurrentUserTokenWithResponse(ctx)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON500, resp.JSON501)
}

// ListAccountSessions lists one page of the account's sign-in sessions, browser
// and CLI alike. The session this client's token came from is marked current.
func (c *Client) ListAccountSessions(ctx context.Context, page, limit int) (*AccountSessionsPage, error) {
	resp, err := c.client.ListAccountSessionsWithResponse(ctx, &apiclient.ListAccountSessionsParams{
		Page:  &page,
		Limit: &limit,
	})
	if err != nil {
		return nil, err
	}
	body, err := apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON500, resp.JSON501)
	if err != nil {
		return nil, err
	}
	result := &AccountSessionsPage{Page: page, Limit: limit}
	if body.Sessions != nil {
		result.Sessions = *body.Sessions
	}
	if body.Page != nil {
		result.Page = *body.Page
	}
	if body.Limit != nil {
		result.Limit = *body.Limit
	}
	if body.Total != nil {
		result.Total = *body.Total
	}
	if body.TotalPages != nil {
		result.TotalPages = *body.TotalPages
	}
	return result, nil
}

// RevokeAccountSession revokes one of the account's sessions and the platform
// tokens created from it. A session that is not the account's answers 404.
func (c *Client) RevokeAccountSession(ctx context.Context, sessionID string) error {
	resp, err := c.client.RevokeAccountSessionWithResponse(ctx, sessionID)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON404, resp.JSON500, resp.JSON501)
}

// RevokeOtherAccountSessions revokes every session of the account except the
// one this client's token came from, browser sessions included. A token with
// no session of its own ends them all.
func (c *Client) RevokeOtherAccountSessions(ctx context.Context) error {
	resp, err := c.client.RevokeOtherAccountSessionsWithResponse(ctx)
	if err != nil {
		return err
	}
	return apiOK(resp.StatusCode(), resp.Body, resp.JSON401, resp.JSON403, resp.JSON500, resp.JSON501)
}
