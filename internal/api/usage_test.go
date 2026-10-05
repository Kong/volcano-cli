package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProjectUsage(t *testing.T) {
	projectID := uuid.MustParse("eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550")
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"project_id":"eac37d5a-5f6f-42d8-acf6-0f2ae9c7a550","month":"2026-09","metrics":[{"metric":"Requests","total":4,"all_time":10,"hourly":[],"daily":[]}]}`))
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "pk-account", WithHTTPClient(server.Client()))
	require.NoError(t, err)

	usage, err := client.GetProjectUsage(context.Background(), projectID)
	require.NoError(t, err)
	assert.Equal(t, "/projects/"+projectID.String()+"/usage", gotPath)
	assert.Equal(t, "2026-09", usage.Month)
	assert.Equal(t, int64(4), usage.Metrics[0].Total)
	assert.Equal(t, int64(10), usage.Metrics[0].AllTime)
}
