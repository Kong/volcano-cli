package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var durableTestApprovalID = uuid.MustParse("77777777-7777-4777-8777-777777777777")

// The decision body is optional on the wire. A comment travels in it, and no
// comment is an empty object rather than an empty string, which the history
// would record as a comment someone left blank.
func TestDecideDurableApprovalSendsTheComment(t *testing.T) {
	for _, tc := range []struct {
		name    string
		approve bool
		comment string
		want    map[string]any
	}{
		{name: "approve with a comment", approve: true, comment: "Checked stock", want: map[string]any{"comment": "Checked stock"}},
		{name: "deny without one", approve: false, comment: "", want: map[string]any{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			var body map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				path = r.URL.Path
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				writeAPIJSON(t, w, http.StatusOK, durableApprovalAPIPayload())
			}))
			defer server.Close()

			client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
			require.NoError(t, err)

			decide, action := client.DenyDurableApproval, "deny"
			if tc.approve {
				decide, action = client.ApproveDurableApproval, "approve"
			}
			approval, err := decide(context.Background(), durableTestProjectID, durableTestApprovalID, tc.comment)
			require.NoError(t, err)
			assert.Equal(t, durableTestApprovalID, approval.Id)
			assert.Equal(t, "/projects/"+durableTestProjectID.String()+"/durable-approvals/"+
				durableTestApprovalID.String()+"/"+action, path)
			assert.Equal(t, tc.want, body)
		})
	}
}

// A 409 carries the API's reason, which callers need to tell a conflicting
// decision from any other failure.
func TestDecideDurableApprovalSurfacesTheConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAPIJSON(t, w, http.StatusConflict, map[string]any{
			"error": "approval already decided", "code": "approval_decided",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.ApproveDurableApproval(context.Background(), durableTestProjectID, durableTestApprovalID, "")
	require.Error(t, err)
	assert.Equal(t, http.StatusConflict, Status(err))
	assert.Equal(t, "approval already decided", Message(err))
}

// An unset filter is no filter: sending an empty status or function would ask
// the API for approvals matching the empty string.
func TestListDurableApprovalsSendsOnlyTheFiltersSet(t *testing.T) {
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeAPIJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{}, "page": 1, "limit": 20, "total": 0, "has_more": false,
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.ListDurableApprovals(context.Background(), durableTestProjectID,
		DurableApprovalListInput{Page: 1, Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{"page": {"1"}, "limit": {"20"}}, query)

	from := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	_, err = client.ListDurableApprovals(context.Background(), durableTestProjectID, DurableApprovalListInput{
		Function: "order-pipeline", Status: "pending", From: &from, Page: 1, Limit: 20,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"order-pipeline"}, query["function"])
	assert.Equal(t, []string{"pending"}, query["status"])
	assert.Equal(t, []string{"2026-10-05T12:00:00Z"}, query["from"])
}

// The API measures a missing `to` from its own clock, so a caller that names
// both ends has to get both onto the wire.
func TestGetDurableApprovalStatsSendsTheWindow(t *testing.T) {
	var query map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		writeAPIJSON(t, w, http.StatusOK, map[string]any{
			"from": "2026-09-06T12:00:00Z", "to": "2026-10-06T12:00:00Z",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "token", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	_, err = client.GetDurableApprovalStats(context.Background(), durableTestProjectID, "", nil, nil)
	require.NoError(t, err)
	assert.Empty(t, query)

	from := time.Date(2025, 10, 5, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	_, err = client.GetDurableApprovalStats(context.Background(), durableTestProjectID, "order-pipeline", &from, &to)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{
		"function": {"order-pipeline"},
		"from":     {"2025-10-05T12:00:00Z"},
		"to":       {"2026-10-06T12:00:00Z"},
	}, query)
}

func durableApprovalAPIPayload() map[string]any {
	return map[string]any{
		"id":           durableTestApprovalID.String(),
		"status":       "approved",
		"name":         "ship-order",
		"title":        "Ship order 4417?",
		"description":  "",
		"function":     map[string]any{"id": "55555555-5555-4555-8555-555555555555", "name": "order-pipeline"},
		"execution":    map[string]any{"id": durableTestExecutionID.String(), "name": "order-4417", "status": "running"},
		"requested_at": "2026-10-06T11:00:00Z",
		"expires_at":   nil,
		"decision":     nil,
	}
}
