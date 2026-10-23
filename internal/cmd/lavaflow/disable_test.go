package lavaflow

import (
	"encoding/json"
	"net/http"
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
	assert.Equal(t, map[string]any{"enabled": false}, fake.lastUpdate(t))
	assert.Contains(t, out, "LavaFlow is off")
	fake.set(func(s *lavaflowServer) { assert.Equal(t, "diagnose", s.settings["max_stage"]) })
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

// Turning LavaFlow off reads nothing first, so it works while the settings
// read fails, and keeps the saved stage and trigger.
func TestDisableNeedsNoSettingsRead(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.settings["auto_trigger"] = false
		s.refuse = http.StatusServiceUnavailable
		s.refuseMethod = http.MethodGet
		s.refusal = map[string]any{"error": "subscription pricing temporarily unavailable"}
	})

	out, err := executeLavaflowCommand(t, New(deps), "disable")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"enabled": false}, fake.lastUpdate(t))
	assert.Contains(t, out, "LavaFlow is off")
	fake.set(func(s *lavaflowServer) { assert.Equal(t, false, s.settings["auto_trigger"]) })
}
