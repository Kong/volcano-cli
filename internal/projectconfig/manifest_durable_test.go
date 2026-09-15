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
	assert.Equal(t, []string{"hello"}, manifest.StandardFunctionNames())

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

// A kind the CLI does not know reads as a standard function everywhere
// downstream, so `kind: durabel` would deploy through the standard collection —
// and a kind is fixed at creation, which makes that name the wrong kind of
// function permanently. `standard` is a real value the API's enum carries, so
// it is accepted rather than lumped in with the typos.
func TestManifestRejectsUnknownFunctionKind(t *testing.T) {
	_, err := Parse([]byte("version: 1\nfunctions:\n  - name: hello\n    kind: durabel\n"), noEnv)

	require.ErrorContains(t, err, `function "hello": unsupported kind "durabel"`)
}

func TestManifestAcceptsAnExplicitStandardKind(t *testing.T) {
	manifest, err := Parse([]byte("version: 1\nfunctions:\n  - name: hello\n    kind: standard\n"), noEnv)

	require.NoError(t, err)
	assert.Equal(t, []string{"hello"}, manifest.StandardFunctionNames())
	assert.Empty(t, manifest.DurableFunctionNames())
}

func TestDurableFunctionNamesWithoutFunctions(t *testing.T) {
	manifest, err := Parse([]byte("version: 1\n"), noEnv)
	require.NoError(t, err)
	assert.Empty(t, manifest.DurableFunctionNames())
	assert.Empty(t, manifest.StandardFunctionNames())

	var missing *Manifest
	assert.Empty(t, missing.DurableFunctionNames())
	assert.Empty(t, missing.StandardFunctionNames())
}
