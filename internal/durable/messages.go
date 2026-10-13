package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

// SendMessage sends a named message to one execution. created is false when
// idempotencyKey already sent one; that message is returned and nothing new is
// sent.
func (s Service) SendMessage(
	ctx context.Context, identifier string, executionID uuid.UUID,
	name string, data json.RawMessage, idempotencyKey string,
) (*apiclient.DurableMessage, bool, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, false, err
	}

	message, created, err := authenticated.API.SendDurableMessage(
		ctx, authenticated.ProjectID, identifier, executionID, name, data, idempotencyKey)
	if err != nil {
		if api.Status(err) == http.StatusConflict && api.Code(err) == "execution_not_running" {
			return nil, false, fmt.Errorf("execution %s has ended and takes no more messages", executionID)
		}
		return nil, false, durableExecutionError("send a message to", identifier, executionID, err)
	}
	return message, created, nil
}

// ListMessages reads one execution's messages, oldest first.
func (s Service) ListMessages(
	ctx context.Context, identifier string, executionID uuid.UUID, input api.DurableMessageListInput,
) (*apiclient.DurableMessageList, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	messages, err := authenticated.API.ListDurableMessages(
		ctx, authenticated.ProjectID, identifier, executionID, input)
	if err != nil {
		return nil, durableExecutionError("list the messages of", identifier, executionID, err)
	}
	return messages, nil
}

// ListMessageWaits returns the waits one execution has open, oldest first.
func (s Service) ListMessageWaits(
	ctx context.Context, identifier string, executionID uuid.UUID,
) (*apiclient.DurableMessageWaitList, error) {
	authenticated, err := s.sessions.CurrentProject()
	if err != nil {
		return nil, err
	}

	waits, err := authenticated.API.ListDurableMessageWaits(ctx, authenticated.ProjectID, identifier, executionID)
	if err != nil {
		return nil, durableExecutionError("list the message waits of", identifier, executionID, err)
	}
	return waits, nil
}

// ExecutionEnded reports whether an execution is past the point of receiving
// or publishing messages.
func ExecutionEnded(status apiclient.DurableExecutionStatus) bool {
	return status != apiclient.DurableExecutionStatusPending && status != apiclient.DurableExecutionStatusRunning
}
