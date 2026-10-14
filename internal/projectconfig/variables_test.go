package projectconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVariablesOnlyKeepsJustTheVariableSections(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
project:
  name: notes
variables:
  - name: API_URL
    value: http://localhost:8000
shared_variables: [API_URL]
frontend_shared_variables: []
functions:
  - name: hello
    visibility: public
`), noEnv)
	require.NoError(t, err)
	assert.True(t, manifest.DeclaresVariables())

	subset, ok := manifest.VariablesOnly()
	require.True(t, ok)
	body, err := subset.uploadBody()
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"version": 1,
		"variables": [{"name": "API_URL", "value": "http://localhost:8000"}],
		"shared_variables": ["API_URL"],
		"frontend_shared_variables": []
	}`, string(body))
	assert.Nil(t, subset.Functions)
	assert.Nil(t, subset.Project)
}

func TestVariablesOnlyLeavesOmittedSectionsOut(t *testing.T) {
	manifest, err := Parse([]byte("version: 1\nshared_variables: [LOG_LEVEL]\n"), noEnv)
	require.NoError(t, err)
	assert.False(t, manifest.DeclaresVariables())

	subset, ok := manifest.VariablesOnly()
	require.True(t, ok)
	body, err := subset.uploadBody()
	require.NoError(t, err)
	assert.JSONEq(t, `{"version": 1, "shared_variables": ["LOG_LEVEL"]}`, string(body))
}

func TestVariablesOnlyWithoutVariableSections(t *testing.T) {
	manifest, err := Parse([]byte("version: 1\nfunctions:\n  - name: hello\n"), noEnv)
	require.NoError(t, err)

	_, ok := manifest.VariablesOnly()
	assert.False(t, ok)
}
