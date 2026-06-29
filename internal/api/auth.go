// Package api is the HTTP client for the Volcano cloud API.
package api

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const deviceGrantType = apiclient.UrnIetfParamsOauthGrantTypeDeviceCode

// DeviceTokenPollResult is the normalized result of a device-token poll.
type DeviceTokenPollResult struct {
	AccessToken      string
	Error            string
	ErrorDescription string
}

// ValidateToken validates the configured token by listing projects.
func (c *Client) ValidateToken(ctx context.Context) error {
	_, err := c.ListProjects(ctx, DefaultPage, 1)
	return err
}

// StartDeviceAuthorization starts the OAuth device authorization flow.
func (c *Client) StartDeviceAuthorization(ctx context.Context, clientID string) (*apiclient.DeviceAuthorizationResponse, error) {
	resp, err := c.client.AuthDeviceAuthorizeWithResponse(ctx, apiclient.AuthDeviceAuthorizeJSONRequestBody{
		ClientId: strings.TrimSpace(clientID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start device authorization: %w", err)
	}
	if resp.JSON200 != nil {
		return resp.JSON200, nil
	}
	if resp.JSON400 != nil {
		return nil, oauthError(resp.StatusCode(), resp.JSON400)
	}
	return nil, apiError(resp.StatusCode(), resp.Body)
}

// PollDeviceToken polls the OAuth device token endpoint once.
func (c *Client) PollDeviceToken(ctx context.Context, clientID, deviceCode string) (*DeviceTokenPollResult, error) {
	resp, err := c.client.AuthDeviceTokenWithResponse(ctx, apiclient.AuthDeviceTokenJSONRequestBody{
		ClientId:   strings.TrimSpace(clientID),
		DeviceCode: strings.TrimSpace(deviceCode),
		GrantType:  deviceGrantType,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to poll device token: %w", err)
	}
	if resp.JSON200 != nil {
		return &DeviceTokenPollResult{
			AccessToken: resp.JSON200.AccessToken,
		}, nil
	}
	if resp.JSON400 != nil {
		status := &DeviceTokenPollResult{
			Error: resp.JSON400.Error,
		}
		if resp.JSON400.ErrorDescription != nil {
			status.ErrorDescription = *resp.JSON400.ErrorDescription
		}
		return status, nil
	}

	return nil, apiError(resp.StatusCode(), resp.Body)
}

// ExchangePlatformToken exchanges an auth-user device-flow token for a platform token.
func (c *Client) ExchangePlatformToken(ctx context.Context, authAccessToken, clientID string) (*apiclient.PlatformExchangeResponse, error) {
	resp, err := c.client.AuthPlatformExchangeWithResponse(
		ctx,
		apiclient.AuthPlatformExchangeJSONRequestBody{ClientId: strings.TrimSpace(clientID)},
		authorizationEditor(authAccessToken),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange platform token: %w", err)
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON403)
}

// WebSignupURL builds the Volcano Web signup URL used by the CLI signup flow.
func WebSignupURL(webURL, email, next string) (string, error) {
	webURL = strings.TrimRight(strings.TrimSpace(webURL), "/")
	if webURL == "" {
		return "", errors.New("web url cannot be empty")
	}
	parsed, err := url.Parse(webURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse web url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("web url must use http:// or https:// scheme")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/signup"
	query := parsed.Query()
	if email = strings.TrimSpace(email); email != "" {
		query.Set("email", email)
	}
	if next = strings.TrimSpace(next); next != "" {
		query.Set("next", next)
	}
	query.Set("source", "cli")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// VerificationWebTarget extracts the web origin and device-approval path advertised
// by a device-authorization response. The signup flow uses these so the browser is
// sent to the same environment that issued the device code — mirroring how login
// follows the API's verification URI. Both values are empty when the response does
// not carry a usable verification URI.
func VerificationWebTarget(deviceAuth *apiclient.DeviceAuthorizationResponse) (origin, devicePath string) {
	if deviceAuth == nil {
		return "", ""
	}
	base, err := url.Parse(strings.TrimSpace(deviceAuth.VerificationUri))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", ""
	}
	origin = base.Scheme + "://" + base.Host

	// verification_uri_complete already carries the prefilled user_code, so prefer it.
	if complete, err := url.Parse(strings.TrimSpace(deviceAuth.VerificationUriComplete)); err == nil && complete.Path != "" {
		return origin, complete.RequestURI()
	}

	devicePath = base.Path
	if devicePath == "" {
		devicePath = "/device"
	}
	if userCode := strings.TrimSpace(deviceAuth.UserCode); userCode != "" {
		values := base.Query()
		values.Set("user_code", userCode)
		devicePath += "?" + values.Encode()
	}
	return origin, devicePath
}
