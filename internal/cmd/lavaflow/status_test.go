package lavaflow

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

func TestStatusOffSaysWhatLavaFlowDoesAndHowToTurnItOn(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow: off")
	assert.Contains(t, out, "Autonomy: open fix pull requests")
	assert.Contains(t, out, "Repository: "+lavaflowRepo)
	assert.Contains(t, out, "Custom context: not set")
	assert.Contains(t, out, clilavaflow.OnDescription)
	assert.Contains(t, out, "Turn it on with volcano cloud lavaflow enable.")
	assertLavaFlowWording(t, out)
}

func TestStatusOnWithoutARepositoryAsksForOne(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.settings["custom_context"] = "Checkout runs on Fridays only."
		s.repoConnected = false
	})

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow: on")
	assert.Contains(t, out, "Repository: none connected")
	assert.Contains(t, out, "Custom context: set (30 characters)")
	assert.Contains(t, out, "Connect a repository so LavaFlow can open fix pull requests: volcano git connect")
	assert.NotContains(t, out, "Turn it on")
}

// Diagnosing needs no repository, and a connection that can't be read is
// left out rather than reported missing.
func TestStatusAsksForARepositoryOnlyWhenItOpensPullRequests(t *testing.T) {
	for name, change := range map[string]func(*lavaflowServer){
		"diagnose only": func(s *lavaflowServer) {
			s.settings["enabled"], s.settings["autonomy"], s.repoConnected = true, "diagnose_only", false
		},
		"unreadable connection": func(s *lavaflowServer) {
			s.settings["enabled"], s.repoFailure = true, http.StatusInternalServerError
		},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(change)

		out, err := executeLavaflowCommand(t, New(deps), "status")
		require.NoError(t, err, name)
		assert.NotContains(t, out, "Connect a repository", name)
	}
}

func TestStatusJSONSaysTheSettingsAndTheRepository(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.repoConnected = false })

	out, err := executeLavaflowCommand(t, New(deps), "status", "--json")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"settings":         defaultLavaflowSettings(),
		"repository_state": clilavaflow.RepositoryNotConnected,
	}, decodeJSON(t, out))
}
