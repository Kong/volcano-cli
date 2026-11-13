package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
	Resource  logRequestResource `json:"resource"`
	Limit     *int               `json:"limit,omitempty"`
	Cursor    *string            `json:"cursor,omitempty"`
	Q         string             `json:"q,omitempty"`
	StartTime *time.Time         `json:"start_time,omitempty"`
	EndTime   *time.Time         `json:"end_time,omitempty"`
}

type logRequestResource struct {
	Type        string                        `json:"type"`
	IDs         []uuid.UUID                   `json:"ids,omitempty"`
	Kind        string                        `json:"kind,omitempty"`
	Deployments *logDeploymentRequestSelector `json:"deployments,omitempty"`
}

// LogQuery selects project runtime logs: one resource type, optionally narrowed
// to some resources of it, plus the search filters every log route shares.
type LogQuery struct {
	// Type is "function", "frontend" or "database".
	Type string
	// IDs narrows the read to these resources; empty reads every one.
	IDs []uuid.UUID
	// Kind narrows functions to "standard" or "durable"; empty reads both.
	Kind string
	// Q is a log query, such as `level:error timeout`.
	Q     string
	Start *time.Time
	End   *time.Time
}

func (q LogQuery) resource() logRequestResource {
	return logRequestResource{Type: q.Type, IDs: q.IDs, Kind: q.Kind}
}

// SearchLogs returns one page of project runtime logs matching query, newest
// first.
func (c *Client) SearchLogs(ctx context.Context, projectID uuid.UUID, query LogQuery, limit int, cursor string) (*apiclient.LogSearchResponse, error) {
	body := logSearchRequest{
		Resource:  query.resource(),
		Q:         query.Q,
		StartTime: query.Start,
		EndTime:   query.End,
	}
	if limit > 0 {
		body.Limit = &limit
	}
	if cursor != "" {
		body.Cursor = &cursor
	}
	return c.searchProjectLogs(ctx, projectID, body)
}

// StreamLogs live-tails project runtime logs matching query. A stream has no
// end time, so query.End is not sent.
func (c *Client) StreamLogs(ctx context.Context, projectID uuid.UUID, query LogQuery, limit int, lastEventID string) (*ProjectLogStream, error) {
	body := logSearchRequest{
		Resource:  query.resource(),
		Q:         query.Q,
		StartTime: query.Start,
	}
	if limit > 0 {
		body.Limit = &limit
	}
	return c.streamProjectLogs(ctx, projectID, body, lastEventID)
}

type logActivityRequest struct {
	Resource    logRequestResource `json:"resource"`
	Q           string             `json:"q,omitempty"`
	StartTime   *time.Time         `json:"start_time,omitempty"`
	EndTime     *time.Time         `json:"end_time,omitempty"`
	BucketCount *int               `json:"bucket_count,omitempty"`
}

// LogActivity returns bucketed counts of the project runtime logs matching
// query. The body is hand-marshalled for the same reason as logSearchRequest.
func (c *Client) LogActivity(ctx context.Context, projectID uuid.UUID, query LogQuery, buckets int) (*apiclient.LogActivityResponse, error) {
	body := logActivityRequest{
		Resource:  query.resource(),
		Q:         query.Q,
		StartTime: query.Start,
		EndTime:   query.End,
	}
	if buckets > 0 {
		body.BucketCount = &buckets
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal log activity request: %w", err)
	}
	resp, err := c.client.GetProjectLogActivityWithBodyWithResponse(ctx, projectID, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	return apiResult(resp.StatusCode(), resp.Body, resp.JSON200, resp.JSON400, resp.JSON401, resp.JSON403, resp.JSON404)
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
