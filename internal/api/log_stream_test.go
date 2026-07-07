package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamProjectLogsRequestAndEvents(t *testing.T) {
	projectID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	functionID := uuid.MustParse("22222222-2222-4222-8222-222222222222")
	deploymentID := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	var body map[string]any
	var lastEventID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/volcano-api/projects/"+projectID.String()+"/logs/stream", r.URL.Path)
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		assert.Equal(t, "text/event-stream", r.Header.Get("Accept"))
		lastEventID = r.Header.Get("Last-Event-ID")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(": connected\n\n"))
		_, _ = w.Write([]byte("id: next-id\n"))
		_, _ = w.Write([]byte("event: log\n"))
		_, _ = w.Write([]byte(`data: {"id":"log-1","message":"build finished","timestamp":"2026-07-06T12:00:00Z","resource":{"type":"function","id":"` + functionID.String() + `"},"deployment":{"id":"` + deploymentID.String() + `"}}` + "\n\n"))
		_, _ = w.Write([]byte("event: warning\n"))
		_, _ = w.Write([]byte(`data: {"error":"temporary read failure"}` + "\n\n"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/volcano-api", "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	stream, err := client.StreamFunctionDeploymentLogs(context.Background(), projectID, functionID, deploymentID, 50, "previous-id")
	require.NoError(t, err)
	defer stream.Close()

	assert.Equal(t, "previous-id", lastEventID)
	resource, ok := body["resource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "function", resource["type"])
	assert.Equal(t, []any{functionID.String()}, resource["ids"])
	deployments, ok := resource["deployments"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, []any{deploymentID.String()}, deployments["ids"])
	assert.InEpsilon(t, 50, body["limit"], 0)
	assert.NotContains(t, body, "cursor")

	event, err := stream.Next()
	require.NoError(t, err)
	require.NotNil(t, event.Log)
	assert.Equal(t, "next-id", event.ID)
	assert.Equal(t, "log-1", event.Log.Id)
	assert.Equal(t, "build finished", event.Log.Message)

	event, err = stream.Next()
	require.NoError(t, err)
	assert.Equal(t, "temporary read failure", event.Warning)
}

func TestStreamProjectLogsNon2xxReturnsAPIError(t *testing.T) {
	projectID := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	frontendID := uuid.MustParse("44444444-4444-4444-8444-444444444444")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIJSON(t, w, http.StatusServiceUnavailable, map[string]string{"error": "logs unavailable"})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	stream, err := client.StreamFrontendLogs(context.Background(), projectID, frontendID, 100, "")
	require.Nil(t, stream)
	var apiErr *Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
	assert.Contains(t, apiErr.Message, "logs unavailable")
}
