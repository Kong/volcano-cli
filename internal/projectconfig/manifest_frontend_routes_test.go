package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// frontendRoutesExport is what a server with Frontend routes renders: the key
// follows function_routes, and only on a frontend that has routes.
const frontendRoutesExport = `version: 1
functions:
  - name: session
    visibility: public
    invocation_mode: http
    http_auth_mode: none
frontends:
  - name: site
    function_routes:
      - function: session
        path_prefix: /api/session
        strip_prefix: true
    frontend_routes:
      - frontend: docs
        path_prefix: /docs
      - frontend: docs
        path_prefix: /blog
  - name: docs
`

// A pulled export has to deploy again, and the manifest decodes strictly, so
// frontend_routes must be known here or a project using it fails to parse.
func TestManifestRoundTripsFrontendRoutes(t *testing.T) {
	pulled, removed, err := sanitizePulledManifest([]byte(frontendRoutesExport))
	require.NoError(t, err)
	assert.Empty(t, removed)

	manifest, err := Parse(pulled, noEnv)
	require.NoError(t, err)

	require.NotNil(t, manifest.Frontends)
	require.Len(t, *manifest.Frontends, 2)
	routes := (*manifest.Frontends)[0].FrontendRoutes
	require.NotNil(t, routes)
	assert.Equal(t, []FrontendRouteManifest{
		{Frontend: "docs", PathPrefix: "/docs"},
		{Frontend: "docs", PathPrefix: "/blog"},
	}, *routes)
	assert.Nil(t, (*manifest.Frontends)[1].FrontendRoutes)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var uploaded, exported map[string]any
	require.NoError(t, json.Unmarshal(encoded, &uploaded))
	var source map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(frontendRoutesExport), &source))
	sourceJSON, err := json.Marshal(source)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(sourceJSON, &exported))
	assert.Equal(t, exported, uploaded)
}

// An empty list removes every route while an omitted one keeps them, so the two
// must reach the server distinguishably.
func TestManifestUploadDistinguishesEmptyAndOmittedFrontendRoutes(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
frontends:
  - name: site
    frontend_routes: []
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
	assert.Equal(t, []any{}, body.Frontends[0]["frontend_routes"])
	assert.NotContains(t, body.Frontends[1], "frontend_routes")
}

func TestManifestInterpolatesFrontendRoutes(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
frontends:
  - name: site
    frontend_routes:
      - frontend: ${DOCS_FRONTEND}
        path_prefix: /docs
`), func(name string) (string, bool) {
		if name == "DOCS_FRONTEND" {
			return "docs-v2", true
		}
		return "", false
	})
	require.NoError(t, err)

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body struct {
		Frontends []map[string]any `json:"frontends"`
	}
	require.NoError(t, json.Unmarshal(encoded, &body))
	assert.Equal(t, []any{map[string]any{"frontend": "docs-v2", "path_prefix": "/docs"}}, body.Frontends[0]["frontend_routes"])
}

func TestManifestRejectsUnknownFrontendRouteFields(t *testing.T) {
	_, err := Parse([]byte(`version: 1
frontends:
  - name: site
    frontend_routes:
      - frontend: docs
        path_prefix: /docs
        strip_prefix: true
`), noEnv)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "field strip_prefix not found")
}

func TestManifestDeclaresFrontendRoutes(t *testing.T) {
	for name, tc := range map[string]struct {
		manifest string
		want     bool
	}{
		"none":            {"version: 1\nfrontends:\n  - name: site\n", false},
		"function routes": {"version: 1\nfrontends:\n  - name: site\n    function_routes: []\n", false},
		"empty":           {"version: 1\nfrontends:\n  - name: site\n    frontend_routes: []\n", true},
		"listed": {
			"version: 1\nfrontends:\n  - name: site\n    frontend_routes:\n      - frontend: docs\n        path_prefix: /docs\n",
			true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			manifest, err := Parse([]byte(tc.manifest), noEnv)
			require.NoError(t, err)
			assert.Equal(t, tc.want, manifest.DeclaresFrontendRoutes())
			assert.NotContains(t, manifest.VisibilityLevelFields(), "frontend_routes")
		})
	}
}
