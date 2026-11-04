package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A project that lets its frontend preview origins pass CORS exports
// allow_frontend_previews, so a pulled manifest has to parse and push back
// unchanged.
func TestParseAuthCORSAllowFrontendPreviews(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
auth:
  cors:
    enabled: true
    allowed_origins:
      - https://app.example.com
    allow_credentials: true
    allow_frontend_previews: true
`), noEnv)
	require.NoError(t, err)

	cors := manifest.Auth.Cors
	require.NotNil(t, cors)
	require.NotNil(t, cors.AllowFrontendPreviews)
	assert.True(t, *cors.AllowFrontendPreviews)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))

	auth, ok := body["auth"].(map[string]any)
	require.True(t, ok)
	uploaded, ok := auth["cors"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, true, uploaded["allow_frontend_previews"])
}

// Leaving the key out keeps the server's value, so it must not be sent.
func TestParseAuthCORSOmitsAllowFrontendPreviewsWhenUndeclared(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
auth:
  cors:
    enabled: true
`), noEnv)
	require.NoError(t, err)
	assert.Nil(t, manifest.Auth.Cors.AllowFrontendPreviews)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))
	assert.NotContains(t, body["auth"].(map[string]any)["cors"], "allow_frontend_previews")
}
