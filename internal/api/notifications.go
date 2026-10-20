package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// GetNotificationSettings returns where a project's alerts go and when a
// failing scheduler is announced. The webhook secret is never part of it, but
// the webhook URL can carry the receiver's own token, so an unexpected body is
// not echoed into the error.
func (c *Client) GetNotificationSettings(ctx context.Context, projectID uuid.UUID) (*apiclient.NotificationSettings, error) {
	resp, err := c.client.GetNotificationSettingsWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), errorFieldsOnly(resp.Body), resp.JSON200, resp.JSON403, resp.JSON404)
}

// UpdateNotificationSettings changes the fields body sets. The response
// carries the webhook signing secret when the update generated one.
func (c *Client) UpdateNotificationSettings(
	ctx context.Context, projectID uuid.UUID, body apiclient.UpdateNotificationSettingsRequest,
) (*apiclient.NotificationSettings, error) {
	resp, err := c.client.UpdateNotificationSettingsWithResponse(ctx, projectID, body)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), errorFieldsOnly(resp.Body), resp.JSON200, resp.JSON400, resp.JSON403, resp.JSON404)
}

// RotateNotificationWebhookSecret replaces the webhook signing secret and
// returns the settings carrying the new one.
func (c *Client) RotateNotificationWebhookSecret(ctx context.Context, projectID uuid.UUID) (*apiclient.NotificationSettings, error) {
	resp, err := c.client.RotateNotificationWebhookSecretWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), errorFieldsOnly(resp.Body), resp.JSON200, resp.JSON403, resp.JSON404, resp.JSON409)
}

// SendTestNotification sends a test notification on every configured channel
// and reports how each delivery went.
func (c *Client) SendTestNotification(ctx context.Context, projectID uuid.UUID) (*apiclient.NotificationTestResult, error) {
	resp, err := c.client.SendTestNotificationWithResponse(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403, resp.JSON404, resp.JSON409, resp.JSON429)
}
