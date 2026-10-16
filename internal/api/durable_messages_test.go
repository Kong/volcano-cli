package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const durableTestMessagesPath = "/projects/22222222-2222-4222-8222-222222222222" +
	"/durable-functions/order-pipeline/executions/66666666-6666-4666-8666-666666666666/messages"

func durableMessageAPIPayload() map[string]any {
	return map[string]any{
		"id":           "88888888-8888-4888-8888-888888888888",
		"execution_id": durableTestExecutionID.String(),
		"direction":    "in",
		"seq":          3,
		"name":         "payment-confirmed",
		"data":         map[string]any{"amount": 4200},
		"sender":       map[string]any{"type": "project", "user_id": nil},
		"status":       "queued",
		"sent_at":      "2026-10-12T09:00:00Z",
		"delivered_at": nil,
	}
}

// Data is sent as written, the key travels as a header, and a 201 is a new
// message where a 200 is the one an earlier send with that key stored.
func TestSendDurableMessageWireShape(t *testing.T) {
	for _, tc := range []struct {
		name        string
		data        json.RawMessage
		key         string
		status      int
		wantBody    string
		wantCreated bool
	}{
		{
			name: "new message with data and a key", data: json.RawMessage(`{"amount":12345678901234567890}`),
			key: "pay-4417", status: http.StatusCreated,
			wantBody: `{"name":"payment-confirmed","data":{"amount":12345678901234567890}}`, wantCreated: true,
		},
		{
			name: "repeat without data", status: http.StatusOK,
			wantBody: `{"name":"payment-confirmed"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			var key string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, durableTestMessagesPath, r.URL.Path)
				key = r.Header.Get("Idempotency-Key")
				var err error
				body, err = io.ReadAll(r.Body)
				assert.NoError(t, err)
				writeAPIJSON(t, w, tc.status, durableMessageAPIPayload())
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			message, created, err := client.SendDurableMessage(context.Background(), durableTestProjectID,
				"order-pipeline", durableTestExecutionID, "payment-confirmed", tc.data, tc.key)
			require.NoError(t, err)
			assert.Equal(t, tc.wantCreated, created)
			assert.Equal(t, int64(3), message.Seq)
			assert.JSONEq(t, tc.wantBody, string(body))
			assert.Equal(t, tc.key, key)
		})
	}
}

func TestSendDurableMessageSurfacesTheConflictCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIJSON(t, w, http.StatusConflict, map[string]any{
			"error": "durable execution is not running", "code": "execution_not_running",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, _, err = client.SendDurableMessage(context.Background(), durableTestProjectID,
		"order-pipeline", durableTestExecutionID, "payment-confirmed", nil, "")
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, Status(err))
	assert.Equal(t, "execution_not_running", Code(err))
}

// An unset filter is no filter, so none of them may reach the query string as
// an empty or zero value.
func TestListDurableMessagesSendsOnlySetFilters(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input DurableMessageListInput
		want  map[string][]string
	}{
		{name: "no filters", want: map[string][]string{}},
		{
			name:  "every filter",
			input: DurableMessageListInput{Direction: "out", Name: "progress", After: 7, Limit: 20},
			want: map[string][]string{
				"direction": {"out"}, "name": {"progress"}, "after": {"7"}, "limit": {"20"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var query map[string][]string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, durableTestMessagesPath, r.URL.Path)
				query = r.URL.Query()
				writeAPIJSON(t, w, http.StatusOK, map[string]any{
					"data": []any{durableMessageAPIPayload()}, "has_more": false,
				})
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			list, err := client.ListDurableMessages(context.Background(), durableTestProjectID,
				"order-pipeline", durableTestExecutionID, tc.input)
			require.NoError(t, err)
			require.Len(t, list.Data, 1)
			assert.Equal(t, tc.want, query)
		})
	}
}

func TestListDurableMessageWaitsReadsTheWaits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/projects/22222222-2222-4222-8222-222222222222/durable-functions/order-pipeline"+
			"/executions/66666666-6666-4666-8666-666666666666/message-waits", r.URL.Path)
		writeAPIJSON(t, w, http.StatusOK, map[string]any{"data": []any{map[string]any{
			"id": "99999999-9999-4999-8999-999999999999", "name": "payment-confirmed", "status": "waiting",
			"created_at": "2026-10-12T09:00:00Z", "expires_at": nil,
		}}})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	waits, err := client.ListDurableMessageWaits(context.Background(), durableTestProjectID,
		"order-pipeline", durableTestExecutionID)
	require.NoError(t, err)
	require.Len(t, waits.Data, 1)
	assert.Equal(t, "payment-confirmed", waits.Data[0].Name)
	assert.Nil(t, waits.Data[0].ExpiresAt)
}
