package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// SucceedDurableCallback resumes the step waiting on token with result. The
// outcome is `succeeded` once the execution has it, or `delivering` when
// Volcano recorded it and is still handing it over.
//
// The token is the only credential the callback routes take, so c needs none.
func (c *Client) SucceedDurableCallback(
	ctx context.Context, token string, result json.RawMessage,
) (*apiclient.DurableCallbackOutcome, error) {
	body, err := callbackBody(apiclient.SucceedDurableCallbackJSONRequestBody{Token: token, Result: result})
	if err != nil {
		return nil, err
	}
	resp, err := c.client.SucceedDurableCallbackWithBodyWithResponse(ctx, "application/json", body)
	if err != nil {
		return nil, err
	}
	if resp.JSON202 != nil {
		return resp.JSON202, nil
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON404, resp.JSON409, resp.JSON410, resp.JSON413, resp.JSON429)
}

// FailDurableCallback resumes the step waiting on token by raising an error
// with errorType and message. An empty message is left out.
func (c *Client) FailDurableCallback(
	ctx context.Context, token, errorType, message string,
) (*apiclient.DurableCallbackOutcome, error) {
	failure := apiclient.DurableCallbackError{Type: errorType}
	if message != "" {
		failure.Message = &message
	}
	body, err := callbackBody(apiclient.FailDurableCallbackJSONRequestBody{Token: token, Error: failure})
	if err != nil {
		return nil, err
	}
	resp, err := c.client.FailDurableCallbackWithBodyWithResponse(ctx, "application/json", body)
	if err != nil {
		return nil, err
	}
	if resp.JSON202 != nil {
		return resp.JSON202, nil
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON404, resp.JSON409, resp.JSON410, resp.JSON413, resp.JSON429)
}

// HeartbeatDurableCallback tells the step waiting on token that the work is
// still going, restarting its heartbeat timeout.
func (c *Client) HeartbeatDurableCallback(ctx context.Context, token string) (*apiclient.DurableCallbackOutcome, error) {
	resp, err := c.client.HeartbeatDurableCallbackWithResponse(ctx, apiclient.HeartbeatDurableCallbackJSONRequestBody{
		Token: token,
	})
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON404, resp.JSON409, resp.JSON410, resp.JSON413, resp.JSON429, resp.JSON503)
}

// callbackBody encodes an answer without HTML escaping. Escaping turns each
// <, > or & into six bytes, so a result or message well inside its limit could
// otherwise push the request past the route's body limit.
func callbackBody(answer any) (io.Reader, error) {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(answer); err != nil {
		return nil, fmt.Errorf("failed to encode the callback answer: %w", err)
	}
	return &body, nil
}
