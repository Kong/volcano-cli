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
	assert.Equal(t, map[string]any{"enabled": true}, fake.lastUpdate(t))
	assert.Contains(t, out, "LavaFlow is on")
	assert.Contains(t, out, clilavaflow.OnDescription)
	assert.Contains(t, out, clilavaflow.CostNote)
	assertLavaFlowWording(t, out)
}

// The update changes only what it sends, so enable sends enabled alone and
// what a project configuration narrowed stays as it was.
func TestEnableKeepsTheSavedStageAndTrigger(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["max_stage"] = "diagnose"
		s.settings["auto_trigger"] = false
	})

	out, err := executeLavaflowCommand(t, New(deps), "enable")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"enabled": true}, fake.lastUpdate(t))
	assert.Contains(t, out, "opens no pull requests (max_stage: diagnose")
	assert.Contains(t, out, "runs only when a run is started by hand (auto_trigger: false")
	assert.NotContains(t, out, clilavaflow.OnDescription, "the output describes the settings it kept")
}

func TestEnableExplainsAPlanRefusal(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusForbidden
		s.refuseMethod = http.MethodPatch
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
		s.refuseMethod = http.MethodPatch
		s.refusal = map[string]any{"error": "Project not found.", "code": "project_not_found"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "enable")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Project not found.")
	assert.NotContains(t, err.Error(), "paid plan")
	assert.NotContains(t, err.Error(), "isn't available")
	assert.Equal(t, "project_not_found", api.Code(err))
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
