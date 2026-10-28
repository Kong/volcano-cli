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

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const durableTestCallbackToken = "vcb_0000000000000000000000000000000000000000000"

// Each answer posts the token in the body to its own route, and a 202 is an
// outcome still being delivered rather than an error.
func TestDurableCallbackWireShape(t *testing.T) {
	for _, tc := range []struct {
		name       string
		path       string
		status     int
		answer     func(*Client) (*apiclient.DurableCallbackOutcome, error)
		wantBody   string
		wantStatus apiclient.DurableCallbackStatus
	}{
		{
			name: "succeed", path: "/durable-callbacks/succeed", status: http.StatusOK,
			answer: func(c *Client) (*apiclient.DurableCallbackOutcome, error) {
				return c.SucceedDurableCallback(context.Background(), durableTestCallbackToken,
					json.RawMessage(`{"amount":12345678901234567890}`))
			},
			wantBody:   `{"token":"` + durableTestCallbackToken + `","result":{"amount":12345678901234567890}}`,
			wantStatus: apiclient.DurableCallbackStatusSucceeded,
		},
		{
			name: "succeed still delivering", path: "/durable-callbacks/succeed", status: http.StatusAccepted,
			answer: func(c *Client) (*apiclient.DurableCallbackOutcome, error) {
				return c.SucceedDurableCallback(context.Background(), durableTestCallbackToken, json.RawMessage(`0`))
			},
			wantBody:   `{"token":"` + durableTestCallbackToken + `","result":0}`,
			wantStatus: apiclient.DurableCallbackStatusDelivering,
		},
		{
			name: "fail with a message", path: "/durable-callbacks/fail", status: http.StatusOK,
			answer: func(c *Client) (*apiclient.DurableCallbackOutcome, error) {
				return c.FailDurableCallback(context.Background(), durableTestCallbackToken,
					"CardDeclined", "Insufficient funds")
			},
			wantBody: `{"token":"` + durableTestCallbackToken +
				`","error":{"type":"CardDeclined","message":"Insufficient funds"}}`,
			wantStatus: apiclient.DurableCallbackStatusFailed,
		},
		{
			name: "fail still delivering, without a message", path: "/durable-callbacks/fail", status: http.StatusAccepted,
			answer: func(c *Client) (*apiclient.DurableCallbackOutcome, error) {
				return c.FailDurableCallback(context.Background(), durableTestCallbackToken, "CardDeclined", "")
			},
			wantBody:   `{"token":"` + durableTestCallbackToken + `","error":{"type":"CardDeclined"}}`,
			wantStatus: apiclient.DurableCallbackStatusDelivering,
		},
		{
			name: "heartbeat", path: "/durable-callbacks/heartbeat", status: http.StatusOK,
			answer: func(c *Client) (*apiclient.DurableCallbackOutcome, error) {
				return c.HeartbeatDurableCallback(context.Background(), durableTestCallbackToken)
			},
			wantBody:   `{"token":"` + durableTestCallbackToken + `"}`,
			wantStatus: apiclient.DurableCallbackStatusWaiting,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, tc.path, r.URL.Path)
				assert.Empty(t, r.Header.Get("Authorization"), "the token is the only credential")
				var err error
				body, err = io.ReadAll(r.Body)
				assert.NoError(t, err)
				writeAPIJSON(t, w, tc.status, map[string]any{"status": tc.wantStatus})
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			outcome, err := tc.answer(client)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, outcome.Status)
			assert.JSONEq(t, tc.wantBody, string(body))
		})
	}
}

// An answer is sent without HTML escaping, which would grow each <, > and & to
// six bytes and could push an answer inside its limit past the body limit.
func TestDurableCallbackAnswersAreNotHTMLEscaped(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(*Client) (*apiclient.DurableCallbackOutcome, error)
		want   string
	}{
		{
			name: "succeed",
			answer: func(c *Client) (*apiclient.DurableCallbackOutcome, error) {
				return c.SucceedDurableCallback(context.Background(), durableTestCallbackToken,
					json.RawMessage(`{"note":"<b>A & B</b>"}`))
			},
			want: `"note":"<b>A & B</b>"`,
		},
		{
			name: "fail",
			answer: func(c *Client) (*apiclient.DurableCallbackOutcome, error) {
				return c.FailDurableCallback(context.Background(), durableTestCallbackToken,
					"Rejected", "<b>A & B</b>")
			},
			want: `"message":"<b>A & B</b>"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
				var err error
				body, err = io.ReadAll(r.Body)
				assert.NoError(t, err)
				writeAPIJSON(t, w, http.StatusOK, map[string]any{"status": "succeeded"})
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			_, err = tc.answer(client)
			require.NoError(t, err)
			assert.Contains(t, string(body), tc.want)
		})
	}
}

// The refusals that share a status are told apart by their code.
func TestDurableCallbackSurfacesTheRefusalCode(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{status: http.StatusNotFound, code: "callback_not_found"},
		{status: http.StatusConflict, code: "callback_completed"},
		{status: http.StatusGone, code: "callback_expired"},
		{status: http.StatusGone, code: "execution_ended"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				writeAPIJSON(t, w, tc.status, map[string]any{"error": "refused", "code": tc.code})
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			_, err = client.HeartbeatDurableCallback(context.Background(), durableTestCallbackToken)
			require.Error(t, err)
			assert.Equal(t, tc.status, Status(err))
			assert.Equal(t, tc.code, Code(err))
		})
	}
}
