package api

import "context"

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
