package lavaflow

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
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
	assert.Contains(t, out, "Updated: not saved yet")
	assert.Contains(t, out, clilavaflow.OnDescription)
	assert.Contains(t, out, "Turn it on with volcano cloud lavaflow enable.")
	assertLavaFlowWording(t, out)
}

// LavaFlow opens fix pull requests at open_pr and at merge, so both ask for a
// repository.
func TestStatusOnWithoutARepositoryAsksForOne(t *testing.T) {
	for _, autonomy := range []string{"open_pr", "merge"} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.settings["enabled"], s.settings["autonomy"] = true, autonomy
			s.settings["custom_context"] = "Checkout runs on Fridays only."
			s.repoConnected = false
		})

		out, err := executeLavaflowCommand(t, New(deps), "status")
		require.NoError(t, err, autonomy)
		assert.Contains(t, out, "LavaFlow: on", autonomy)
		assert.Contains(t, out, "Repository: none connected", autonomy)
		assert.Contains(t, out, "Custom context: set (30 characters)", autonomy)
		assert.Contains(t, out, "Connect a repository so LavaFlow can open fix pull requests: volcano git connect", autonomy)
		assert.NotContains(t, out, "Turn it on", autonomy)
	}
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
		"available":        true,
		"repository_state": clilavaflow.RepositoryNotConnected,
	}, decodeJSON(t, out))
}

// A caller who can't change the settings is told so, and isn't told to turn
// LavaFlow on.
func TestStatusSaysWhenTheCallerCantChangeIt(t *testing.T) {
	_, deps := newLavaflowServer(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "You can change it: no")
	assert.Contains(t, out, clilavaflow.OnDescription)
	assert.NotContains(t, out, "Turn it on")
}

// The status says whether autonomy merge can be chosen, from the project's
// lavaflow.merge capability. Any status but available is unavailable, and an
// API that doesn't list merging leaves it out rather than guess.
func TestStatusSaysWhetherLavaFlowCanMerge(t *testing.T) {
	for capability, want := range map[string]any{
		"available":   true,
		"unavailable": false,
		"limited":     false,
		"":            nil,
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.mergeCapability = capability })

		out, err := executeLavaflowCommand(t, New(deps), "status")
		require.NoError(t, err, "%q", capability)
		switch want {
		case true:
			assert.Contains(t, out, "Merging: available\n", "%q", capability)
		case false:
			assert.Contains(t, out, "Merging: not available for this project yet\n", "%q", capability)
		default:
			assert.NotContains(t, out, "Merging:", "%q", capability)
		}
		assertLavaFlowWording(t, out)

		out, err = executeLavaflowCommand(t, New(deps), "status", "--json")
		require.NoError(t, err, "%q", capability)
		got, listed := decodeJSON(t, out)["merge_available"]
		assert.Equal(t, want != nil, listed, "%q", capability)
		assert.Equal(t, want, got, "%q", capability)
	}
}

// A capabilities read that fails leaves merging unknown, as it does LavaFlow's
// own availability.
func TestStatusLeavesMergingOutWhenTheCapabilitiesCantBeRead(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.mergeCapability, s.capabilitiesFailure = "available", http.StatusInternalServerError
	})

	out, err := executeLavaflowCommand(t, New(deps), "status", "--json")
	require.NoError(t, err)
	status := decodeJSON(t, out)
	assert.NotContains(t, status, "merge_available")
	assert.NotContains(t, status, "available")
}
