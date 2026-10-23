package lavaflow

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisableTurnsLavaFlowOffAndKeepsTheOtherSettings(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.settings["max_stage"] = "diagnose"
	})

	out, err := executeLavaflowCommand(t, New(deps), "disable")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"enabled": false, "max_stage": "diagnose", "auto_trigger": true}, fake.lastUpdate(t))
	assert.Contains(t, out, "LavaFlow is off")
}

func TestDisableJSON(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["enabled"] = true })

	out, err := executeLavaflowCommand(t, New(deps), "disable", "--json")
	require.NoError(t, err)
	var settings map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &settings))
	assert.Equal(t, false, settings["enabled"])
}
