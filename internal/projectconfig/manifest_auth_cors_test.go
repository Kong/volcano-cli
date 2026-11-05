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

func TestManifestDeclaresAllowFrontendPreviews(t *testing.T) {
	for name, tc := range map[string]struct {
		manifest string
		want     bool
	}{
		"no auth":  {"version: 1\n", false},
		"no cors":  {"version: 1\nauth:\n  email: {}\n", false},
		"omitted":  {"version: 1\nauth:\n  cors:\n    enabled: true\n", false},
		"false":    {"version: 1\nauth:\n  cors:\n    allow_frontend_previews: false\n", true},
		"declared": {"version: 1\nauth:\n  cors:\n    allow_frontend_previews: true\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			manifest, err := Parse([]byte(tc.manifest), noEnv)
			require.NoError(t, err)
			assert.Equal(t, tc.want, manifest.DeclaresAllowFrontendPreviews())
		})
	}
}
