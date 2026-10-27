package lavaflow

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

func TestStatusOffSaysHowToTurnItOn(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow: off")
	assert.Contains(t, out, "Plan: allows LavaFlow")
	assert.Contains(t, out, "Repository: "+lavaflowRepo)
	assert.Contains(t, out, clilavaflow.CostNote)
	assert.Contains(t, out, "Turn it on: volcano cloud lavaflow enable")
	assertLavaFlowWording(t, out)
}

func TestStatusOnWithoutRepositoryAsksForOne(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.repoConnected = false
	})

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow: on")
	assert.Contains(t, out, "Repository: not connected")
	assert.Contains(t, out, "Connect a repository so LavaFlow can open fix pull requests: volcano git connect")
	assert.NotContains(t, out, "Turn it on")
}

func TestStatusNamesWhatTheProjectConfigurationNarrowed(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.settings["max_stage"] = "diagnose"
		s.settings["auto_trigger"] = false
		s.repoConnected = false
	})

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "opens no pull requests (max_stage: diagnose")
	assert.Contains(t, out, "runs only when a run is started by hand (auto_trigger: false")
	assert.NotContains(t, out, "Connect a repository", "diagnosing needs no repository")
}

// The first row says the whole state, so it reads at a glance.
func TestStatusSaysTheStateAtAGlance(t *testing.T) {
	for _, tc := range []struct {
		change func(*lavaflowServer)
		want   string
	}{
		{func(*lavaflowServer) {}, "LavaFlow: off\n"},
		{func(s *lavaflowServer) { s.settings["enabled"] = true }, "LavaFlow: on, opening fix pull requests in " + lavaflowRepo + "\n"},
		{
			func(s *lavaflowServer) { s.settings["enabled"], s.repoConnected = true, false },
			"LavaFlow: on, diagnosing only until a repository is connected\n",
		},
		{
			func(s *lavaflowServer) { s.settings["enabled"], s.settings["max_stage"] = true, "diagnose" },
			"LavaFlow: on, diagnosing only (max_stage: diagnose)\n",
		},
		{
			func(s *lavaflowServer) { s.settings["enabled"], s.settings["plan_allows_lavaflow"] = true, false },
			"LavaFlow: on, but no run starts until the plan allows it\n",
		},
		{
			func(s *lavaflowServer) { s.settings["enabled"], s.repoFailure = true, http.StatusInternalServerError },
			"LavaFlow: on\n",
		},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(tc.change)

		out, err := executeLavaflowCommand(t, New(deps), "status")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(out, tc.want), "got %q, want it to start with %q", out, tc.want)
	}
}

// The saved settings apply the next time LavaFlow is turned on, so the status
// names them while it is off, without saying it is running.
func TestStatusOffStillNamesTheSavedLimits(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["max_stage"] = "diagnose"
		s.settings["auto_trigger"] = false
	})

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow: off")
	assert.Contains(t, out, "When it is on, LavaFlow diagnoses issues but opens no pull requests (max_stage: diagnose")
	assert.Contains(t, out, "When it is on, LavaFlow runs only when a run is started by hand (auto_trigger: false")
	assert.Contains(t, out, "Turn it on: volcano cloud lavaflow enable")
}

// A connection that could not be read is reported as unknown, never as none.
func TestStatusRepositoryUnknown(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.repoFailure = http.StatusInternalServerError
	})

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Repository: unknown")
	assert.NotContains(t, out, "Connect a repository")

	out, err = executeLavaflowCommand(t, New(deps), "status", "--json")
	require.NoError(t, err)
	var status map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &status))
	assert.Equal(t, "unknown", status["repository_state"])
	assert.NotContains(t, status, "repository")
}

func TestStatusPlanThatDoesNotAllowLavaFlow(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.settings["plan_allows_lavaflow"] = false
	})

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "Plan: does not allow LavaFlow; it needs a paid plan")
	assert.Contains(t, out, "No run starts until the project's plan allows LavaFlow")
}

// The plan refuses turning LavaFlow on, so the hint waits for the upgrade.
func TestStatusOffOnAPlanThatDoesNotAllowLavaFlow(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["plan_allows_lavaflow"] = false })

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "After upgrading, turn it on: volcano cloud lavaflow enable")
	assert.NotContains(t, out, "\nTurn it on")
}

// A cancel during the repository read ends the command, rather than report
// the repository as unknown.
func TestStatusHonorsACancelDuringTheRepositoryRead(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fake.set(func(s *lavaflowServer) { s.onRepo = cancel })

	cmd := New(deps)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"status"})
	err := cmd.ExecuteContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, out.String(), "LavaFlow:")
}

func TestStatusJSON(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "status", "--json")
	require.NoError(t, err)
	var status map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &status))
	assert.Equal(t, lavaflowRepo, status["repository"])
	assert.Equal(t, "connected", status["repository_state"])
	settings, _ := status["settings"].(map[string]any)
	assert.Equal(t, false, settings["enabled"])
}

// A server in local mode refuses LavaFlow with 501, and the reason reaches the
// user with the refusal's status for the exit code.
func TestStatusReportsTheLocalModeRefusal(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusNotImplemented
		s.refusal = map[string]any{"error": "LavaFlow is not available in local mode"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "status")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LavaFlow is not available in local mode")
	assert.Equal(t, http.StatusNotImplemented, api.Status(err))
}

// LavaFlow works only in the repository it was turned on for. Status says
// which changed, that no run starts, and how to resume, in each of the shapes
// the settings take.
func TestStatusSaysLavaFlowIsPausedWhenTheRepositoryChanged(t *testing.T) {
	for name, tc := range map[string]struct {
		enabledRepository map[string]any
		connected         bool
		want              string
	}{
		"another repository": {
			map[string]any{"id": 7, "full_name": "acme/old"},
			true,
			"Warning: The project's repository changed from acme/old since LavaFlow was turned on, " +
				"so no run starts until someone turns LavaFlow on again.\n" +
				"LavaFlow is in preview. During the preview, it needs the SUPERAGENT plan.\n" +
				"Resume LavaFlow with " + lavaflowRepo + ": volcano cloud lavaflow enable\n",
		},
		"one connected after it was turned on with none": {
			nil, true,
			"Warning: A repository was connected after LavaFlow was turned on, " +
				"so no run starts until someone turns LavaFlow on again.\n" +
				"LavaFlow is in preview. During the preview, it needs the SUPERAGENT plan.\n" +
				"Resume LavaFlow with " + lavaflowRepo + ": volcano cloud lavaflow enable\n",
		},
		"none any more": {
			map[string]any{"id": 7, "full_name": "acme/old"},
			false,
			"Warning: acme/old is no longer connected to the project, " +
				"so no run starts until someone turns LavaFlow on again.\n" +
				"LavaFlow is in preview. During the preview, it needs the SUPERAGENT plan.\n" +
				"Resume LavaFlow without a repository: volcano cloud lavaflow enable\n" +
				"Or connect a repository first: volcano git connect\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.settings["enabled"] = true
				s.settings["repository_changed"] = true
				if tc.enabledRepository != nil {
					s.settings["enabled_repository"] = tc.enabledRepository
				}
				s.repoConnected = tc.connected
			})

			out, err := executeLavaflowCommand(t, New(deps), "status")
			require.NoError(t, err)
			assert.Contains(t, out, "LavaFlow: on, but paused until it is turned on again\n")
			assert.True(t, strings.HasSuffix(out, tc.want), out)
			assert.NotContains(t, out, "Connect a repository so LavaFlow can open fix pull requests",
				"the paused line already says what to do")
			assertLavaFlowWording(t, out)
		})
	}
}

func TestStatusIsNotPausedInTheRepositoryLavaFlowWasTurnedOnFor(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.settings["repository_changed"] = false
		s.settings["enabled_repository"] = map[string]any{"id": 1, "full_name": lavaflowRepo}
	})

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.NotContains(t, out, "paused")
}

// Settings saved while off keep the flag, which says nothing until LavaFlow is
// on again.
func TestStatusOffIsNotPaused(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["repository_changed"] = true })

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.NotContains(t, out, "paused")
	assert.Contains(t, out, "Turn it on: volcano cloud lavaflow enable")
}

func TestStatusJSONCarriesTheRepositoryLavaFlowWasTurnedOnFor(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["enabled"] = true
		s.settings["repository_changed"] = true
		s.settings["enabled_repository"] = map[string]any{"id": 7, "full_name": "acme/old"}
	})

	out, err := executeLavaflowCommand(t, New(deps), "status", "--json")
	require.NoError(t, err)
	assert.Contains(t, out, `"repository_changed": true`)
	assert.Contains(t, out, `"full_name": "acme/old"`)
}
