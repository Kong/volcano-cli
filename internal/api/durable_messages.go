package api

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// DurableMessageListInput filters one read of an execution's messages. Empty
// and zero fields apply no filter.
type DurableMessageListInput struct {
	Direction string
	Name      string
	After     int64
	Limit     int
}

// SendDurableMessage sends a named message to an execution. created is false
// when idempotencyKey already sent one: the API returns that message and sends
// nothing. A nil data sends the message with no data, which the workflow reads
// as null.
func (c *Client) SendDurableMessage(
	ctx context.Context, projectID uuid.UUID, function string, executionID uuid.UUID,
	name string, data json.RawMessage, idempotencyKey string,
) (message *apiclient.DurableMessage, created bool, err error) {
	params := &apiclient.SendDurableMessageParams{}
	if idempotencyKey != "" {
		params.IdempotencyKey = &idempotencyKey
	}

	resp, err := c.client.SendDurableMessageWithResponse(ctx, projectID, function, executionID, params,
		apiclient.SendDurableMessageJSONRequestBody{Name: name, Data: data})
	if err != nil {
		return nil, false, err
	}
	if resp.JSON201 != nil {
		return resp.JSON201, true, nil
	}
	message, err = apiResult(resp.StatusCode(), resp.Body, resp.JSON200,
		resp.JSON400, resp.JSON404, resp.JSON409, resp.JSON413, resp.JSON429)
	return message, false, err
}

// ListDurableMessages reads an execution's messages in both directions, oldest
// first.
func (c *Client) ListDurableMessages(
	ctx context.Context, projectID uuid.UUID, function string, executionID uuid.UUID,
	input DurableMessageListInput,
) (*apiclient.DurableMessageList, error) {
	params := &apiclient.ListDurableMessagesParams{}
	if input.Direction != "" {
		direction := apiclient.DurableMessageDirection(input.Direction)
		params.Direction = &direction
	}
	if input.Name != "" {
		name := input.Name
		params.Name = &name
	}
	if input.After > 0 {
		after := input.After
		params.After = &after
	}
	if input.Limit > 0 {
		limit := input.Limit
		params.Limit = &limit
	}

	resp, err := c.client.ListDurableMessagesWithResponse(ctx, projectID, function, executionID, params)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON404)
}

// ListDurableMessageWaits returns the waits an execution has open, oldest
// first.
func (c *Client) ListDurableMessageWaits(
	ctx context.Context, projectID uuid.UUID, function string, executionID uuid.UUID,
) (*apiclient.DurableMessageWaitList, error) {
	resp, err := c.client.ListDurableMessageWaitsWithResponse(ctx, projectID, function, executionID)
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON404)
}
