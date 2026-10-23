package lavaflow

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

func TestEnableTurnsLavaFlowOnAndSaysWhatThatMeans(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "enable")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"enabled": true, "max_stage": "pull_request", "auto_trigger": true}, fake.lastUpdate(t))
	assert.Contains(t, out, "LavaFlow is on")
	assert.Contains(t, out, clilavaflow.OnDescription)
	assert.Contains(t, out, clilavaflow.CostNote)
	assertLavaFlowWording(t, out)
}

// The API replaces the whole settings object, so what a project configuration
// narrowed is sent back rather than reset to the defaults.
func TestEnableKeepsTheSavedStageAndTrigger(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["max_stage"] = "diagnose"
		s.settings["auto_trigger"] = false
	})

	out, err := executeLavaflowCommand(t, New(deps), "enable")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"enabled": true, "max_stage": "diagnose", "auto_trigger": false}, fake.lastUpdate(t))
	assert.Contains(t, out, "opens no pull requests (max_stage: diagnose")
	assert.Contains(t, out, "runs only when a run is started by hand (auto_trigger: false")
	assert.NotContains(t, out, clilavaflow.OnDescription, "the output describes the settings it kept")
}

// Turning LavaFlow on needs the plan, so a read that could not resolve it
// stops the command rather than guess.
func TestEnableStopsWhenTheSettingsCannotBeRead(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusServiceUnavailable
		s.refuseMethod = http.MethodGet
		s.refusal = map[string]any{"error": "subscription pricing temporarily unavailable"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "enable")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to turn LavaFlow on: reading the current settings")
	fake.set(func(s *lavaflowServer) { assert.Empty(t, s.updates) })
}

func TestEnableExplainsAPlanRefusal(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusForbidden
		s.refuseMethod = http.MethodPut
		s.refusal = map[string]any{"error": "a paid plan is required; LavaFlow is a usage-billed add-on", "code": "plan_required"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "enable")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a paid plan is required")
	assert.Contains(t, err.Error(), "upgrade the project's plan in the dashboard")
	assert.Contains(t, err.Error(), clilavaflow.CostNote)
	assert.Equal(t, http.StatusForbidden, api.Status(err), "the refusal keeps its status for the exit code")
}

func TestEnablePassesOtherRefusalsThrough(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusNotFound
		s.refusal = map[string]any{"error": "LavaFlow is not available", "code": "feature_unavailable"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "enable")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LavaFlow is not available")
	assert.NotContains(t, err.Error(), "paid plan")
	assert.Equal(t, "feature_unavailable", api.Code(err))
}

// Settings already on stay on after a downgrade, and saving them again
// succeeds, so enable says no run starts rather than describe automatic runs.
func TestEnableOnAPlanThatNoLongerAllowsLavaFlow(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["plan_allows_lavaflow"] = false })

	out, err := executeLavaflowCommand(t, New(deps), "enable")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow is on")
	assert.Contains(t, out, "No run starts until the project's plan allows LavaFlow")
	assert.NotContains(t, out, clilavaflow.OnDescription)
}
