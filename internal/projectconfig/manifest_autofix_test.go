package projectconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestAutofixManifestRoundTrip(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
autofix:
  enabled: true
  auto_trigger: false
  max_stage: diagnose
  model: claude-opus-5-5
  provider: volcano
`), noEnv)
	require.NoError(t, err)
	body, err := manifest.uploadBody()
	require.NoError(t, err)
	require.JSONEq(t, `{"version":1,"autofix":{"enabled":true,"auto_trigger":false,"max_stage":"diagnose","model":"claude-opus-5-5","provider":"volcano"}}`, string(body))

	exported, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	reparsed, err := Parse(exported, noEnv)
	require.NoError(t, err)
	reuploaded, err := reparsed.uploadBody()
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(reuploaded))
}

func TestAutofixManifestPreservesOmittedAndExplicitValues(t *testing.T) {
	for name, example := range map[string]struct{ yaml, json string }{
		"omitted section":  {"version: 1\n", `{"version":1}`},
		"empty section":    {"version: 1\nautofix: {}\n", `{"version":1,"autofix":{}}`},
		"explicit false":   {"version: 1\nautofix:\n  enabled: false\n  auto_trigger: false\n", `{"version":1,"autofix":{"enabled":false,"auto_trigger":false}}`},
		"partial settings": {"version: 1\nautofix:\n  max_stage: pull_request\n", `{"version":1,"autofix":{"max_stage":"pull_request"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			manifest, err := Parse([]byte(example.yaml), noEnv)
			require.NoError(t, err)
			body, err := manifest.uploadBody()
			require.NoError(t, err)
			require.JSONEq(t, example.json, string(body))
			exported, err := yaml.Marshal(manifest)
			require.NoError(t, err)
			reparsed, err := Parse(exported, noEnv)
			require.NoError(t, err)
			reuploaded, err := reparsed.uploadBody()
			require.NoError(t, err)
			require.JSONEq(t, example.json, string(reuploaded))
		})
	}
}

func TestAutofixManifestRejectsAPIKey(t *testing.T) {
	_, err := Parse([]byte("version: 1\nautofix:\n  api_key: secret\n"), noEnv)
	require.Error(t, err)
}
