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
