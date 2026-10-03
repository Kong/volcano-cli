package projectconfig

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// serverExport is what a server with function visibility renders for a project
// with a routed function: visibility replaces public, and the frontend carries
// its function routes.
const serverExport = `version: 1
functions:
  - name: nightly
    visibility: private
  - name: reports
    visibility: authenticated
  - name: session
    visibility: public
    invocation_mode: http
    http_auth_mode: none
frontends:
  - name: web
    function_routes:
      - function: session
        path_prefix: /api/session
        strip_prefix: true
      - function: session
        path_prefix: /api/legacy
        strip_prefix: false
`

// A pulled export has to deploy again, and the manifest decodes strictly, so
// both fields must be known here or every project using them fails to parse.
func TestManifestRoundTripsVisibilityAndFunctionRoutes(t *testing.T) {
	pulled, stripped, err := sanitizePulledManifest([]byte(serverExport))
	require.NoError(t, err)
	assert.False(t, stripped)

	manifest, err := Parse(pulled, noEnv)
	require.NoError(t, err)

	require.NotNil(t, manifest.Functions)
	visibility := map[string]string{}
	for _, function := range *manifest.Functions {
		require.NotNil(t, function.Visibility, function.Name)
		assert.Nil(t, function.Public, function.Name)
		visibility[function.Name] = *function.Visibility
	}
	assert.Equal(t, map[string]string{"nightly": "private", "reports": "authenticated", "session": "public"}, visibility)

	require.NotNil(t, manifest.Frontends)
	routes := (*manifest.Frontends)[0].FunctionRoutes
	require.NotNil(t, routes)
	require.Len(t, *routes, 2)
	assert.Equal(t, "session", (*routes)[0].Function)
	assert.Equal(t, "/api/session", (*routes)[0].PathPrefix)
	require.NotNil(t, (*routes)[0].StripPrefix)
	assert.True(t, *(*routes)[0].StripPrefix)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var uploaded, exported map[string]any
	require.NoError(t, json.Unmarshal(encoded, &uploaded))
	var source map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(serverExport), &source))
	sourceJSON, err := json.Marshal(source)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(sourceJSON, &exported))
	assert.Equal(t, exported, uploaded)
}

// An empty list removes every route while an omitted one keeps them, so the two
// must reach the server distinguishably.
func TestManifestUploadDistinguishesEmptyAndOmittedFunctionRoutes(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
frontends:
  - name: web
    function_routes: []
  - name: docs
`), noEnv)
	require.NoError(t, err)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body struct {
		Frontends []map[string]any `json:"frontends"`
	}
	require.NoError(t, json.Unmarshal(encoded, &body))
	require.Len(t, body.Frontends, 2)
	assert.Equal(t, []any{}, body.Frontends[0]["function_routes"])
	assert.NotContains(t, body.Frontends[1], "function_routes")
}

// Manifests written before visibility keep working: public is uploaded as
// written and the server maps it to a level.
func TestManifestStillAcceptsLegacyPublic(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
functions:
  - name: hello
    public: true
  - name: notes-summary
    public: false
`), noEnv)
	require.NoError(t, err)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body struct {
		Functions []map[string]any `json:"functions"`
	}
	require.NoError(t, json.Unmarshal(encoded, &body))
	require.Len(t, body.Functions, 2)
	assert.Equal(t, true, body.Functions[0]["public"])
	assert.Equal(t, false, body.Functions[1]["public"])
	assert.NotContains(t, body.Functions[0], "visibility")
	assert.NotContains(t, body.Functions[1], "visibility")
}

func TestManifestRejectsUnknownFunctionRouteFields(t *testing.T) {
	_, err := Parse([]byte(`version: 1
frontends:
  - name: web
    function_routes:
      - function: session
        path_prefix: /api
        target: session
`), noEnv)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field target not found")
}

// The deploy commands read these to point a new private function at config
// deploy when the manifest already gives it a level.
func TestReadFunctionDeployManifestReadsDeclaredVisibility(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		require.NoError(t, os.WriteFile("volcano-config.yaml", []byte(`version: 1
functions:
  - name: reports
    visibility: Authenticated
  - name: legacy-open
    public: true
  - name: legacy-closed
    public: false
  - name: undeclared
  - name: order-pipeline
    kind: durable
    visibility: private
`), 0o644))

		read, err := ReadFunctionDeployManifest("")
		require.NoError(t, err)
		assert.Equal(t, map[string]string{
			"reports":        "authenticated",
			"legacy-open":    "public",
			"legacy-closed":  "authenticated",
			"order-pipeline": "private",
		}, read.Visibility)
	})
}
