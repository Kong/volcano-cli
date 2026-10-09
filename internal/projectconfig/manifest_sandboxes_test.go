package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func TestSandboxManifestRoundTrip(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
sandboxes:
  - name: local-custom
    memory_mb: 1024
    ports: [8080]
    ttl_seconds: 600
`), noEnv)
	require.NoError(t, err)
	body, err := manifest.uploadBody()
	require.NoError(t, err)
	require.JSONEq(t, `{"version":1,"sandboxes":[{"name":"local-custom","memory_mb":1024,"ports":[8080],"ttl_seconds":600}]}`, string(body))
	var wire apiclient.ProjectConfig
	require.NoError(t, json.Unmarshal(body, &wire))
	require.NotNil(t, wire.Sandboxes)
	require.Len(t, *wire.Sandboxes, 1)
	entry := (*wire.Sandboxes)[0]
	require.Equal(t, "local-custom", entry.Name)
	require.NotNil(t, entry.TtlSeconds)
	require.Equal(t, 600, *entry.TtlSeconds)

	exported, err := yaml.Marshal(manifest)
	require.NoError(t, err)
	reparsed, err := Parse(exported, noEnv)
	require.NoError(t, err)
	reuploaded, err := reparsed.uploadBody()
	require.NoError(t, err)
	require.JSONEq(t, string(body), string(reuploaded))
}

func TestSandboxManifestPreservesOmittedAndExplicitValues(t *testing.T) {
	for name, example := range map[string]struct{ yaml, json string }{
		"omitted section":         {"version: 1\n", `{"version":1}`},
		"empty section":           {"version: 1\nsandboxes: []\n", `{"version":1,"sandboxes":[]}`},
		"omitted settings":        {"version: 1\nsandboxes:\n  - name: custom\n", `{"version":1,"sandboxes":[{"name":"custom"}]}`},
		"zero and empty settings": {"version: 1\nsandboxes:\n  - name: custom\n    ports: []\n    ttl_seconds: 0\n", `{"version":1,"sandboxes":[{"name":"custom","ports":[],"ttl_seconds":0}]}`},
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
			body, err = reparsed.uploadBody()
			require.NoError(t, err)
			require.JSONEq(t, example.json, string(body))
		})
	}
}

func TestSandboxManifestRejectsUnknownFields(t *testing.T) {
	_, err := Parse([]byte("version: 1\nsandboxes:\n  - name: custom\n    ttl_second: 600\n"), noEnv)
	require.ErrorContains(t, err, "field ttl_second not found")
}

func TestSandboxManifestRejectsRemovedIdleTimeout(t *testing.T) {
	_, err := Parse([]byte("version: 1\nsandboxes:\n  - name: custom\n    idle_timeout_seconds: 90\n"), noEnv)
	require.ErrorContains(t, err, "field idle_timeout_seconds not found")
}
