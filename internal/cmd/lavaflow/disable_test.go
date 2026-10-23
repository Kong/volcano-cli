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

// Turning LavaFlow off needs no plan, so an outage of the plan service, which
// fails the settings read, must not keep it on.
func TestDisableWhenTheSettingsCannotBeRead(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.refuse = http.StatusServiceUnavailable
		s.refuseMethod = http.MethodGet
		s.refusal = map[string]any{"error": "subscription pricing temporarily unavailable"}
	})

	out, err := executeLavaflowCommand(t, New(deps), "disable")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"enabled": false}, fake.lastUpdate(t))
	assert.Contains(t, out, "LavaFlow is off")
	assert.Contains(t, out, "max_stage and auto_trigger went back to their defaults")
}
