package projectconfig

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// LavaFlow isn't part of the project configuration: turning it on is a
// person's decision, made with its own commands. A manifest that still
// declares the section is refused, naming its line and the command to use,
// before anything is sent.
func TestManifestRefusesALavaFlowSection(t *testing.T) {
	for name, manifest := range map[string]string{
		"settings": "version: 1\nproject:\n  name: shop\nlavaflow:\n  enabled: true\n  max_stage: diagnose\n",
		"empty":    "version: 1\nproject:\n  name: shop\nlavaflow: {}\n",
	} {
		_, err := Parse([]byte(manifest), noEnv)
		require.ErrorIs(t, err, errLavaFlowSection, name)
		assert.Equal(t, "invalid manifest: line 4: LavaFlow isn't part of the project configuration: remove the lavaflow "+
			"section, and turn LavaFlow on with 'volcano cloud lavaflow enable'", err.Error(), name)
	}
}

// Only the top-level section is refused: a key named lavaflow inside another
// section is whatever that section makes of it.
func TestManifestRefusesOnlyATopLevelLavaFlowSection(t *testing.T) {
	_, err := Parse([]byte("version: 1\nproject:\n  lavaflow: true\n"), noEnv)
	require.Error(t, err)
	assert.NotErrorIs(t, err, errLavaFlowSection)
}
