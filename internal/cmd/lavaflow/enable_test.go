package lavaflow

import (
	"net/http"
	"strings"
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
	assert.Contains(t, out, "LavaFlow is on\nNote: "+clilavaflow.OnDescription+"\n")
	assert.Contains(t, out, "LavaFlow diagnoses the next issue that happens a second time or comes back after you resolve it, and opens a fix pull request in "+lavaflowRepo+".\n")
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
			"LavaFlow diagnoses the next issue that happens a second time or comes back after you resolve it. " +
				"Until you connect a repository, it stops at the diagnosis. Connecting one pauses LavaFlow until you " +
				"turn it on again: volcano git connect, then volcano cloud lavaflow enable\n",
		},
		{
			"unreadable connection",
			func(s *lavaflowServer) { s.repoFailure = http.StatusInternalServerError },
			"LavaFlow diagnoses the next issue that happens a second time or comes back after you resolve it, and opens a fix pull request in your connected repository.\n",
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
	assert.Contains(t, out, "LavaFlow diagnoses each issue you start a run for (auto_trigger: false in the project configuration), "+
		"and opens no pull request (max_stage: diagnose in the project configuration).\n")
	assert.NotContains(t, out, lavaflowRepo, "a diagnose-only run opens no pull request anywhere")
}

func TestEnableExplainsAPlanRefusal(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusForbidden
		s.refuseMethod = http.MethodPatch
		s.refusal = map[string]any{"error": "a paid plan is required", "code": "plan_required"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "enable")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a paid plan is required")
	assert.Contains(t, err.Error(), "LavaFlow needs a paid plan.")
	assert.NotContains(t, err.Error(), "dashboard")
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
	assert.Contains(t, out, clilavaflow.OnDescription, "the AI disclosure is said whatever the plan")
	assert.NotContains(t, out, "LavaFlow diagnoses")
}

// Turning LavaFlow on decides which repository it works in, so a project
// access token is refused before anything is sent, as a permission the API
// refuses. Signing in saves a token, which a token in VOLCANO_TOKEN still
// overrides, so that case says to unset it first.
func TestEnableWithAProjectTokenSaysAPersonHasToTurnItOn(t *testing.T) {
	for name, tc := range map[string]struct {
		token func(t *testing.T)
		want  string
	}{
		"from the environment": {
			func(t *testing.T) {
				t.Helper()
				t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")
			},
			"a person has to turn LavaFlow on: a project access token can't. Unset VOLCANO_TOKEN and sign in " +
				"with 'volcano login', then run this again",
		},
		"saved": {
			func(t *testing.T) {
				t.Helper()
				require.NoError(t, (&cliconfig.Config{
					UserToken:      cliconfig.ProjectTokenPrefix + "secret",
					CurrentProject: &cliconfig.ProjectConfig{ID: lavaflowProjectID, Name: "Shop"},
				}).Save())
			},
			"a person has to turn LavaFlow on: a project access token can't. Sign in with 'volcano login' " +
				"and run this again",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			tc.token(t)

			_, err := executeLavaflowCommand(t, New(deps), "enable")
			require.ErrorIs(t, err, clilavaflow.ErrPersonMustTurnOn)
			require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired, "the exit code reports a permission refused")
			assert.Equal(t, tc.want, err.Error())
			assert.Empty(t, fake.updates)
		})
	}
}
