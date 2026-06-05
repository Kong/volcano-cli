package bucket

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestList(t *testing.T) {
	setBucketCommandTestHome(t)
	saveBucketCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == storageBucketURL {
			writeBucketJSON(t, w, http.StatusOK, []any{
				bucketPayload("uploads"),
				bucketPayload("avatars"),
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBucketCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list")
	require.NoError(t, err)
	assert.Contains(t, out, "uploads")
	assert.Contains(t, out, "Total: 2 bucket(s)")
}

func TestListEmpty(t *testing.T) {
	setBucketCommandTestHome(t)
	saveBucketCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == storageBucketURL {
			writeBucketJSON(t, w, http.StatusOK, []any{})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBucketCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No storage buckets")
}
