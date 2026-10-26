package lavaflow

import (
	"net/http"
	"strings"
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
	assert.Contains(t, out, "LavaFlow is on\n")
	assert.Contains(t, out, "LavaFlow diagnoses the next new or regressed issue and opens a fix pull request in "+lavaflowRepo+".\n")
	assert.Contains(t, out, clilavaflow.CostNote)
	assertLavaFlowWording(t, out)
}

// What happens next depends on the repository LavaFlow's pull requests go
// to, so enable names it, says LavaFlow only diagnoses without one, and stays
// general when the connection cannot be read.
func TestEnableSaysWhereTheFixPullRequestGoes(t *testing.T) {
	for _, tc := range []struct {
		name string
		repo func(*lavaflowServer)
		want string
	}{
		{
			"no repository",
			func(s *lavaflowServer) { s.repoConnected = false },
			"LavaFlow diagnoses the next new or regressed issue. It only diagnoses until a repository is connected: volcano git connect\n",
		},
		{
			"unreadable connection",
			func(s *lavaflowServer) { s.repoFailure = http.StatusInternalServerError },
			"LavaFlow diagnoses the next new or regressed issue and opens a fix pull request in your connected repository.\n",
		},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(tc.repo)

		out, err := executeLavaflowCommand(t, New(deps), "enable")
		require.NoError(t, err, tc.name)
		assert.Contains(t, out, tc.want, tc.name)
		assert.Equal(t, 1, strings.Count(out, "LavaFlow diagnoses"), tc.name)
	}
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
	assert.Contains(t, out, "LavaFlow diagnoses each issue you start a run for (auto_trigger: false in the project configuration) "+
		"and opens no pull request (max_stage: diagnose in the project configuration).\n")
	assert.NotContains(t, out, lavaflowRepo, "a diagnose-only run opens no pull request anywhere")
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
	assert.NotContains(t, out, "LavaFlow diagnoses")
}
