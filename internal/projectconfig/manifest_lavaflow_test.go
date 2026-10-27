package projectconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// A pulled manifest carries the server's lavaflow section, so deploying it
// back has to parse, and the upload has to match the API contract.
func TestManifestLavaFlowSection(t *testing.T) {
	manifest, err := Parse([]byte(`version: 1
lavaflow:
  enabled: true
  max_stage: diagnose
  auto_trigger: false
`), noEnv)
	require.NoError(t, err)
	require.NotNil(t, manifest.LavaFlow)
	assert.True(t, *manifest.LavaFlow.Enabled)
	assert.Equal(t, "diagnose", *manifest.LavaFlow.MaxStage)
	assert.False(t, *manifest.LavaFlow.AutoTrigger)

	body, err := manifest.uploadBody()
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1,"lavaflow":{"enabled":true,"max_stage":"diagnose","auto_trigger":false}}`, string(body))

	var wire apiclient.ProjectConfig
	require.NoError(t, json.Unmarshal(body, &wire))
	require.NotNil(t, wire.Lavaflow, "the API contract has to carry the section the manifest uploads")
	assert.Equal(t, apiclient.LavaFlowStageDiagnose, *wire.Lavaflow.MaxStage)
}

// An omitted field keeps its saved value, so it must not reach the upload.
func TestManifestLavaFlowOmittedFieldsStayOmitted(t *testing.T) {
	manifest, err := Parse([]byte("version: 1\nlavaflow:\n  enabled: false\n"), noEnv)
	require.NoError(t, err)

	body, err := manifest.uploadBody()
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1,"lavaflow":{"enabled":false}}`, string(body))
}

// Volcano picks the models, so the manifest refuses the fields that chose one.
func TestManifestLavaFlowRefusesModelSettings(t *testing.T) {
	for _, field := range []string{"provider: anthropic", "model: some-model", "api_key: secret-value"} {
		t.Run(field, func(t *testing.T) {
			_, err := Parse([]byte("version: 1\nlavaflow:\n  enabled: true\n  "+field+"\n"), noEnv)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not found in type projectconfig.LavaFlowManifest")
		})
	}
}
