package projectconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A manifest pulled from the server carries the scope fields, so strict
// decoding has to accept them or `config pull` output cannot be redeployed.
func TestParseAcceptsFunctionVariableScope(t *testing.T) {
	manifest, err := Parse([]byte(`
version: 1
functions:
  - name: scoped-fn
    variable_scope: scoped
    variables:
      - API_KEY
      - DB_URL
  - name: all-fn
    variable_scope: all
`), noEnv)
	require.NoError(t, err)
	require.NotNil(t, manifest.Functions)
	functions := *manifest.Functions
	require.Len(t, functions, 2)

	require.NotNil(t, functions[0].VariableScope)
	assert.Equal(t, "scoped", *functions[0].VariableScope)
	require.NotNil(t, functions[0].Variables)
	assert.Equal(t, []string{"API_KEY", "DB_URL"}, *functions[0].Variables)

	require.NotNil(t, functions[1].VariableScope)
	assert.Equal(t, "all", *functions[1].VariableScope)
	assert.Nil(t, functions[1].Variables)
}

// An empty declared list is meaningful under `scoped`: it selects only the
// names hosting detects in the source. It must survive as an empty list, not
// collapse into an omitted field.
func TestParseKeepsEmptyVariableList(t *testing.T) {
	manifest, err := Parse([]byte(`
version: 1
functions:
  - name: detected-only
    variable_scope: scoped
    variables: []
`), noEnv)
	require.NoError(t, err)
	functions := *manifest.Functions
	require.NotNil(t, functions[0].Variables)
	assert.Empty(t, *functions[0].Variables)

	body, err := manifest.uploadBody()
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	function := decoded["functions"].([]any)[0].(map[string]any)
	assert.Equal(t, []any{}, function["variables"])
}

// Omitting both fields must upload neither, so the server keeps whatever scope
// the function already has. This is the compatibility guarantee.
func TestUploadOmitsAbsentVariableScope(t *testing.T) {
	manifest, err := Parse([]byte(`
version: 1
functions:
  - name: untouched
    public: true
`), noEnv)
	require.NoError(t, err)

	body, err := manifest.uploadBody()
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	function := decoded["functions"].([]any)[0].(map[string]any)
	assert.NotContains(t, function, "variable_scope")
	assert.NotContains(t, function, "variables")
}

// Config export is server-rendered YAML: whatever hosting returns must parse
// and re-upload unchanged, carrying only the declarations hosting sent.
func TestConfigExportRoundTrip(t *testing.T) {
	exported := []byte(`version: 1
functions:
  - name: scoped-fn
    public: false
    variable_scope: scoped
    variables:
      - API_KEY
  - name: default-fn
    public: true
`)
	manifest, err := Parse(exported, noEnv)
	require.NoError(t, err)

	body, err := manifest.uploadBody()
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	functions := decoded["functions"].([]any)

	scoped := functions[0].(map[string]any)
	assert.Equal(t, "scoped", scoped["variable_scope"])
	assert.Equal(t, []any{"API_KEY"}, scoped["variables"])

	// The function hosting reported without a declaration stays without one.
	other := functions[1].(map[string]any)
	assert.NotContains(t, other, "variable_scope")
	assert.NotContains(t, other, "variables")
}

func TestFunctionVariableDeclarations(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		require.NoError(t, os.WriteFile("volcano-config.yaml", []byte(`version: 1
functions:
  - name: scoped-fn
    variable_scope: scoped
    variables:
      - API_KEY
  - name: plain-fn
    public: true
`), 0o644))

		declarations := FunctionVariableDeclarations("")
		require.Len(t, declarations, 1)

		scoped := declarations["scoped-fn"]
		require.NotNil(t, scoped.VariableScope)
		assert.Equal(t, "scoped", *scoped.VariableScope)
		require.NotNil(t, scoped.Variables)
		assert.Equal(t, []string{"API_KEY"}, *scoped.Variables)

		// A function with no declaration is absent, not present-and-nil.
		_, ok := declarations["plain-fn"]
		assert.False(t, ok)
	})
}

// Function deploy is not a config command, so a missing or broken manifest
// must not fail it — it just yields no declarations.
func TestFunctionVariableDeclarationsTolerantOfBadManifest(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		assert.Nil(t, FunctionVariableDeclarations(""))

		require.NoError(t, os.WriteFile("volcano-config.yaml", []byte("version: 99\n"), 0o644))
		assert.Nil(t, FunctionVariableDeclarations(""))

		require.NoError(t, os.WriteFile("volcano-config.yaml", []byte(":\tnot yaml\n"), 0o644))
		assert.Nil(t, FunctionVariableDeclarations(""))
	})
}

func TestFunctionVariableDeclarationsFromNestedManifest(t *testing.T) {
	withTempWorkingDir(t, func(_ string) {
		require.NoError(t, os.MkdirAll("volcano", 0o755))
		require.NoError(t, os.WriteFile(filepath.Join("volcano", "volcano-config.yaml"), []byte(`version: 1
functions:
  - name: nested-fn
    variable_scope: all
`), 0o644))

		declarations := FunctionVariableDeclarations("")
		require.Len(t, declarations, 1)
		require.NotNil(t, declarations["nested-fn"].VariableScope)
		assert.Equal(t, "all", *declarations["nested-fn"].VariableScope)
		assert.Nil(t, declarations["nested-fn"].Variables)
	})
}
