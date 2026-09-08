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
	got, stripped, err := sanitizePulledManifest([]byte(`# volcano-config.yaml (manifest version 1)
version: 1
variables:
  - name: API_KEY
    value: secret-value
shared_variables:
  - LOG_LEVEL
`))
	require.NoError(t, err)
	assert.True(t, stripped)
	assert.NotContains(t, string(got), "secret-value")

	// The re-marshaled manifest must still read like a canonical export: head
	// comment kept, two-space sequence indent, no document-start marker.
	assert.Equal(t, `# volcano-config.yaml (manifest version 1)
version: 1
shared_variables:
  - LOG_LEVEL
`, string(got))

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
	require.ErrorIs(t, err, ErrUnsafePulledManifest)
	assert.Contains(t, err.Error(), "not valid YAML")
}

func TestSanitizePulledManifestRejectsAdditionalDocuments(t *testing.T) {
	manifest := "version: 1\n---\nvariables:\n  - name: API_KEY\n    value: secret\n"
	_, _, err := sanitizePulledManifest([]byte(manifest))
	require.ErrorIs(t, err, ErrUnsafePulledManifest)
	assert.Contains(t, err.Error(), "multiple YAML documents")
}

func TestSanitizePulledManifestRejectsUnsupportedRoots(t *testing.T) {
	for _, manifest := range []string{
		"- variables: [{name: API_KEY, value: secret}]\n",
		"secret\n",
		"null\n",
	} {
		_, _, err := sanitizePulledManifest([]byte(manifest))
		require.ErrorIs(t, err, ErrUnsafePulledManifest)
		assert.Contains(t, err.Error(), "unsupported YAML root")
	}
}

func TestSanitizePulledManifestReportsEmptyVariablesSection(t *testing.T) {
	got, stripped, err := sanitizePulledManifest([]byte("version: 1\nvariables: []\n"))
	require.NoError(t, err)
	assert.True(t, stripped)
	assert.NotContains(t, string(got), "variables")
}
