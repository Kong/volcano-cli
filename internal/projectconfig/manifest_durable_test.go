package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The manifest decodes strictly, so a declared kind has to be a known field
// here or every project holding a durable function fails to parse. It uploads
// unchanged: the server asserts it against the deployed function rather than
// applying it.
func TestManifestParsesAndUploadsFunctionKind(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
functions:
  - name: hello
  - name: order-pipeline
    kind: durable
`), noEnv)
	require.NoError(t, err)

	assert.Equal(t, []string{"order-pipeline"}, manifest.DurableFunctionNames())

	encoded, err := manifest.uploadBody()
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))

	functions, ok := body["functions"].([]any)
	require.True(t, ok)
	standard, ok := functions[0].(map[string]any)
	require.True(t, ok)
	durable, ok := functions[1].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, standard, "kind")
	assert.Equal(t, "durable", durable["kind"])
}

func TestManifestRejectsUnknownFunctionKind(t *testing.T) {
	_, err := Parse([]byte("version: 1\nfunctions:\n  - name: hello\n    kind: durabel\n"), noEnv)
	require.ErrorContains(t, err, `function "hello": unsupported kind "durabel"`)
}

func TestDurableFunctionNamesWithoutFunctions(t *testing.T) {
	manifest, err := Parse([]byte("version: 1\n"), noEnv)
	require.NoError(t, err)
	assert.Empty(t, manifest.DurableFunctionNames())

	var missing *Manifest
	assert.Empty(t, missing.DurableFunctionNames())
}
