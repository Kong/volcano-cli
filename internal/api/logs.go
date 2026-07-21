package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const (
	logResourceTypeFrontend = "frontend"
	logResourceTypeFunction = "function"
)

// logSearchRequest is a hand-marshalled body for POST /projects/{id}/logs/search,
// sent via SearchProjectLogsWithBodyWithResponse rather than the typed
// SearchProjectLogsWithResponse. The generated apiclient models the request's
// `resource` selector as an oapi-codegen oneOf union (apiclient.LogRequestResource),
// built through From.../As... accessors; this flat struct produces the identical
// wire format while keeping the call sites readable. Intentional — do not
// "simplify" it back to the typed union call.
type logSearchRequest struct {
	Resource logRequestResource `json:"resource"`
	Limit    *int               `json:"limit,omitempty"`
	Cursor   *string            `json:"cursor,omitempty"`
}

type logRequestResource struct {
	Type        string                        `json:"type"`
	IDs         []uuid.UUID                   `json:"ids,omitempty"`
	Deployments *logDeploymentRequestSelector `json:"deployments,omitempty"`
}

type logDeploymentRequestSelector struct {
	IDs []uuid.UUID `json:"ids,omitempty"`
}

func (c *Client) searchProjectLogs(ctx context.Context, projectID uuid.UUID, body logSearchRequest) (*apiclient.LogSearchResponse, error) {
	normalizeLogSearchRequest(&body)

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal log search request: %w", err)
	}

	resp, err := c.client.SearchProjectLogsWithBodyWithResponse(ctx, projectID, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404)
}

func normalizeLogSearchRequest(body *logSearchRequest) {
	if body == nil {
		return
	}
	if body.Limit != nil && *body.Limit <= 0 {
		body.Limit = nil
	}
	if body.Cursor != nil {
		cursor := strings.TrimSpace(*body.Cursor)
		if cursor == "" {
			body.Cursor = nil
		} else {
			body.Cursor = &cursor
		}
	}
}

func logResource(resourceType string, resourceID uuid.UUID) logRequestResource {
	return logRequestResource{
		Type: resourceType,
		IDs:  []uuid.UUID{resourceID},
	}
}

func logDeploymentResource(resourceType string, resourceID, deploymentID uuid.UUID) logRequestResource {
	resource := logResource(resourceType, resourceID)
	resource.Deployments = &logDeploymentRequestSelector{
		IDs: []uuid.UUID{deploymentID},
	}
	return resource
}
