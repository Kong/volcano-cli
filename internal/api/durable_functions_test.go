package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	durableTestProjectID   = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	durableTestExecutionID = uuid.MustParse("66666666-6666-4666-8666-666666666666")
)

// A start body is the execution's input itself rather than a wrapper, and the
// idempotency key travels as a header, so both are easy to get wrong without a
// test pinning the wire shape.
func TestStartDurableExecutionSendsInputAsTheBody(t *testing.T) {
	var body any
	var executionName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t,
			"/projects/"+durableTestProjectID.String()+"/durable-functions/order-pipeline/executions",
			r.URL.Path)
		executionName = r.Header.Get("X-Volcano-Execution-Name")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		writeAPIJSON(t, w, http.StatusAccepted, durableExecutionAPIPayload("pending"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	execution, err := client.StartDurableExecution(context.Background(), durableTestProjectID, "order-pipeline",
		DurableExecutionStartInput{Input: map[string]any{"order_id": 4417}, Name: "order-4417"})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"order_id": float64(4417)}, body)
	assert.Equal(t, "order-4417", executionName)
	assert.Equal(t, durableTestExecutionID, execution.Id)
}

// No input at all is a different thing from an empty object, and no name means
// the platform generates one, so neither may be sent as a made-up value. Only
// an empty body carries "no input": the API reads a JSON null body as an input
// and hands it to the function.
func TestStartDurableExecutionWithoutInputOrName(t *testing.T) {
	var raw []byte
	var hasNameHeader bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hasNameHeader = r.Header["X-Volcano-Execution-Name"]
		var err error
		raw, err = io.ReadAll(r.Body)
		require.NoError(t, err)
		writeAPIJSON(t, w, http.StatusAccepted, durableExecutionAPIPayload("pending"))
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.StartDurableExecution(context.Background(), durableTestProjectID, "order-pipeline",
		DurableExecutionStartInput{})
	require.NoError(t, err)
	assert.Empty(t, raw)
	assert.False(t, hasNameHeader)
}

// A start is refused for reasons the caller has to be able to tell apart: too
// many executions in flight (429) and durable execution being unavailable
// (503) are both retryable, unlike a bad request.
func TestStartDurableExecutionSurfacesRefusalStatuses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		message string
	}{
		{name: "concurrency", status: http.StatusTooManyRequests, message: "too many executions running"},
		{name: "unavailable", status: http.StatusServiceUnavailable, message: "durable execution is unavailable"},
		{name: "payload too large", status: http.StatusRequestEntityTooLarge, message: "input too large"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeAPIJSON(t, w, tc.status, map[string]string{"error": tc.message})
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			_, err = client.StartDurableExecution(context.Background(), durableTestProjectID, "order-pipeline",
				DurableExecutionStartInput{})
			require.Error(t, err)
			assert.Equal(t, tc.status, Status(err))
			assert.ErrorContains(t, err, tc.message)
		})
	}
}

// Deploying is a create-or-redeploy, so both 201 and 200 are successes.
func TestDeployDurableFunctionAcceptsCreatedAndUpdated(t *testing.T) {
	for _, status := range []int{http.StatusCreated, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var fields map[string]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, r.ParseMultipartForm(1024*1024))
				fields = map[string]string{
					"name":      r.FormValue("name"),
					"runtime":   r.FormValue("runtime"),
					"handler":   r.FormValue("handler"),
					"is_public": r.FormValue("is_public"),
				}
				require.Len(t, r.MultipartForm.File["code"], 1)
				writeAPIJSON(t, w, status, durableFunctionAPIPayload())
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			isPublic := true
			fn, err := client.DeployDurableFunction(context.Background(), durableTestProjectID, DurableFunctionDeployInput{
				Name:          "order-pipeline",
				Runtime:       "nodejs24.x",
				Handler:       "handler",
				SourceArchive: []byte("archive"),
				IsPublic:      &isPublic,
			})
			require.NoError(t, err)
			assert.Equal(t, "order-pipeline", fn.Name)
			assert.Equal(t, map[string]string{
				"name": "order-pipeline", "runtime": "nodejs24.x", "handler": "handler", "is_public": "true",
			}, fields)
		})
	}
}

// An absent is_public is what tells the server to keep the visibility the
// function already has, so it must not be sent as false.
func TestDeployDurableFunctionOmitsUnsetVisibility(t *testing.T) {
	var sent []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseMultipartForm(1024*1024))
		sent = r.MultipartForm.Value["is_public"]
		writeAPIJSON(t, w, http.StatusOK, durableFunctionAPIPayload())
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.DeployDurableFunction(context.Background(), durableTestProjectID, DurableFunctionDeployInput{
		Name: "order-pipeline", Runtime: "nodejs24.x", Handler: "handler", SourceArchive: []byte("archive"),
	})
	require.NoError(t, err)
	assert.Empty(t, sent)
}

func TestListDurableExecutionsPassesTheStatusFilter(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		writeAPIJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{durableExecutionAPIPayload("running")},
			"has_more": false,
			"page":     1,
			"limit":    100,
			"total":    1,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	page, err := client.ListDurableExecutions(context.Background(), durableTestProjectID, "order-pipeline",
		"running", 1, 100)
	require.NoError(t, err)
	require.Len(t, page.Data, 1)
	assert.Contains(t, query, "status=running")
}

func durableFunctionAPIPayload() map[string]any {
	return map[string]any{
		"id":               "55555555-5555-4555-8555-555555555555",
		"project_id":       durableTestProjectID.String(),
		"name":             "order-pipeline",
		"runtime":          "nodejs24.x",
		"handler":          "handler",
		"kind":             "durable",
		"status":           "provisioning",
		"is_public":        true,
		"deployed_regions": []string{"aws-us-east-1"},
		"durable": map[string]any{
			"execution_timeout_seconds": 3600,
			"retention_days":            30,
		},
		"created_at": "2026-05-20T00:00:00Z",
		"updated_at": "2026-05-20T00:00:00Z",
	}
}

func durableExecutionAPIPayload(status string) map[string]any {
	return map[string]any{
		"id":                  durableTestExecutionID.String(),
		"durable_function_id": "55555555-5555-4555-8555-555555555555",
		"project_id":          durableTestProjectID.String(),
		"name":                "order-4417",
		"status":              status,
		"region":              "aws-us-east-1",
		"created_at":          "2026-05-20T00:00:00Z",
		"updated_at":          "2026-05-20T00:00:00Z",
	}
}
