package bucket

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestUpdateSetsFields(t *testing.T) {
	setBucketCommandTestHome(t)
	saveBucketCommandTestConfig(t)

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch && r.URL.Path == storageBucketURL+"/uploads" {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
			writeBucketJSON(t, w, http.StatusOK, bucketPayload("uploads"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBucketCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"update", "uploads",
		"--allowed-mime-type", "image/png",
		"--file-size-limit", "4096",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Bucket 'uploads' updated")

	require.NotNil(t, captured)
	assert.EqualValues(t, 4096, captured["file_size_limit"])
	allowed, ok := captured["allowed_mime_types"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"image/png"}, allowed)
}

func TestUpdateRequiresFlag(t *testing.T) {
	setBucketCommandTestHome(t)
	saveBucketCommandTestConfig(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBucketCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"update", "uploads",
	)
	require.Error(t, err)
	assert.Contains(t, out, "at least one of --allowed-mime-type or --file-size-limit is required")
}
