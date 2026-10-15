package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Codes the API returns when it refuses a bootstrap token.
const (
	BootstrapTokenInvalid = "bootstrap_token_invalid"
	BootstrapTokenExpired = "bootstrap_token_expired"
	BootstrapTokenUsed    = "bootstrap_token_used"
)

// maxBootstrapResponseBytes caps the exchange response read into memory.
const maxBootstrapResponseBytes = 64 << 10

// BootstrapExchange is the CLI login a bootstrap token was exchanged for.
type BootstrapExchange struct {
	Token   string `json:"token"`
	UserID  string `json:"user_id"`
	TokenID string `json:"token_id"`
}

// ExchangeBootstrapToken trades a one-time vbt_ token, which the dashboard
// puts in the prompt it shows, for a normal CLI login. The request carries no
// Authorization header: the bootstrap token is the credential.
func (c *Client) ExchangeBootstrapToken(ctx context.Context, token, clientName, hostname string) (*BootstrapExchange, error) {
	payload, err := json.Marshal(map[string]string{
		"token":       strings.TrimSpace(token),
		"client_name": clientName,
		"hostname":    hostname,
	})
	if err != nil {
		return nil, err
	}
	endpoint, err := url.JoinPath(c.baseURL, "auth", "cli-bootstrap-tokens", "exchange")
	if err != nil {
		return nil, fmt.Errorf("failed to build the exchange URL: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange the bootstrap token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBootstrapResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to read the exchange response: %w", err)
	}
	// Any 2xx carries the secret, so only a refusal's body becomes an error.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, apiError(resp.StatusCode, body)
	}
	// The body holds the new login's secret, so a parse failure never
	// repeats it.
	var exchange BootstrapExchange
	if err := json.Unmarshal(body, &exchange); err != nil || strings.TrimSpace(exchange.Token) == "" {
		return nil, errors.New("the exchange response has no token")
	}
	return &exchange, nil
}
