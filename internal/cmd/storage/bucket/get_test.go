package bucket

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestGet(t *testing.T) {
	setBucketCommandTestHome(t)
	saveBucketCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == storageBucketURL+"/uploads" {
			writeBucketJSON(t, w, http.StatusOK, bucketPayload("uploads"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBucketCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "get", "uploads")
	require.NoError(t, err)
	assert.Contains(t, out, "Name: uploads")
	assert.Contains(t, out, "File size limit: 1.0 MiB")
}
