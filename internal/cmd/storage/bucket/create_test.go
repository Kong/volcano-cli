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

func TestCreate(t *testing.T) {
	setBucketCommandTestHome(t)
	saveBucketCommandTestConfig(t)

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == storageBucketURL {
			require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
			writeBucketJSON(t, w, http.StatusCreated, bucketPayload("uploads"))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	out, err := executeBucketCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}),
		"create", "uploads",
		"--allowed-mime-type", "image/png",
		"--allowed-mime-type", "image/jpeg",
		"--file-size-limit", "2048",
	)
	require.NoError(t, err)
	assert.Contains(t, out, "Bucket 'uploads' created")

	require.NotNil(t, captured)
	assert.Equal(t, "uploads", captured["name"])
	assert.EqualValues(t, 2048, captured["file_size_limit"])
	allowed, ok := captured["allowed_mime_types"].([]any)
	require.True(t, ok)
	assert.Equal(t, []any{"image/png", "image/jpeg"}, allowed)
}
