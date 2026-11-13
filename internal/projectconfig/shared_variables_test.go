package projectconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedVariablesUpload(t *testing.T) {
	for _, tc := range []struct{ name, yaml, json string }{
		{"omitted", "version: 1\n", `{"version":1}`},
		{"clear", "version: 1\nshared_variables: []\n", `{"version":1,"shared_variables":[]}`},
		{"names only", "version: 1\nshared_variables: [LOG_LEVEL, Service_URL]\n", `{"version":1,"shared_variables":["LOG_LEVEL","Service_URL"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manifest, err := Parse([]byte(tc.yaml), noEnv)
			require.NoError(t, err)
			if tc.name == "omitted" {
				assert.Nil(t, manifest.SharedVariables)
			} else {
				require.NotNil(t, manifest.SharedVariables)
			}
			body, err := manifest.uploadBody()
			require.NoError(t, err)
			assert.JSONEq(t, tc.json, string(body))
		})
	}
}

func TestSharedVariablesRejectsValueObjects(t *testing.T) {
	_, err := Parse([]byte("version: 1\nshared_variables:\n  - name: API_KEY\n    value: secret\n"), noEnv)
	require.Error(t, err)
}

// A variable entry's own shared is sent as written, and an entry without it
// sends nothing, which leaves the server's default.
func TestVariableSharedUpload(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
variables:
  - name: LOG_LEVEL
    value: debug
  - name: STRIPE_SECRET_KEY
    value: sk_test_fake
    shared: false
  - name: SERVICE_URL
    value: https://example.test
    shared: true
`), noEnv)
	require.NoError(t, err)
	require.NotNil(t, manifest.Variables)
	variables := *manifest.Variables
	assert.Nil(t, variables[0].Shared)
	require.NotNil(t, variables[1].Shared)
	assert.False(t, *variables[1].Shared)
	require.NotNil(t, variables[2].Shared)
	assert.True(t, *variables[2].Shared)

	body, err := manifest.uploadBody()
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1,"variables":[
		{"name":"LOG_LEVEL","value":"debug"},
		{"name":"STRIPE_SECRET_KEY","value":"sk_test_fake","shared":false},
		{"name":"SERVICE_URL","value":"https://example.test","shared":true}
	]}`, string(body))
}

func TestVariableSharedRejectsANonBoolean(t *testing.T) {
	_, err := Parse([]byte("version: 1\nvariables:\n  - name: LOG_LEVEL\n    value: debug\n    shared: sometimes\n"), noEnv)
	require.Error(t, err)
}
