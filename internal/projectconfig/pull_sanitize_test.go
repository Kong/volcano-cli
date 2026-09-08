package projectconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestSanitizePulledManifestKeepsCleanExportsVerbatim(t *testing.T) {
	for _, tc := range []struct{ name, manifest string }{
		{"empty", ""},
		{"blank", "\n\n"},
		{"names only", "# canonical\nversion: 1\nshared_variables:\n  - LOG_LEVEL\n"},
		{"function variable names", "version: 1\nfunctions:\n  - name: hello\n    variables: [LOG_LEVEL]\n"},
		{"nested variables key is not the top-level section", "version: 1\nauth:\n  variables: [A]\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, stripped, err := sanitizePulledManifest([]byte(tc.manifest))
			require.NoError(t, err)
			assert.False(t, stripped)
			assert.Equal(t, tc.manifest, string(got), "clean exports must be saved byte for byte")
		})
	}
}

func TestSanitizePulledManifestDropsVariablesSection(t *testing.T) {
	got, stripped, err := sanitizePulledManifest([]byte(`version: 1
variables:
  - name: API_KEY
    value: secret-value
shared_variables:
  - LOG_LEVEL
`))
	require.NoError(t, err)
	assert.True(t, stripped)
	assert.NotContains(t, string(got), "secret-value")

	// The section is removed outright, not emptied or left value-less: a
	// value-less variables list would fail the server's required-field
	// validation on the next deploy, and an empty one would delete every
	// project variable.
	var saved map[string]any
	require.NoError(t, yaml.Unmarshal(got, &saved))
	assert.NotContains(t, saved, "variables")
	assert.Equal(t, 1, saved["version"])
	assert.Equal(t, []any{"LOG_LEVEL"}, saved["shared_variables"])
}

func TestSanitizePulledManifestRejectsUnparseableYAML(t *testing.T) {
	_, _, err := sanitizePulledManifest([]byte("version: 1\nvariables: [\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not valid YAML")
}
