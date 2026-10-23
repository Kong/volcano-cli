package lavaflow

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
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

// Turning LavaFlow on decides which repository it works in, so a project
// access token is refused before anything is sent.
func TestEnableWithAProjectTokenSaysAPersonHasToTurnItOn(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")

	_, err := executeLavaflowCommand(t, New(deps), "enable")
	require.ErrorIs(t, err, clilavaflow.ErrPersonMustTurnOn)
	assert.Contains(t, err.Error(), "a person has to turn LavaFlow on")
	assert.Empty(t, fake.updates)
}
