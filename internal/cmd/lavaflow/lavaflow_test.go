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

func TestCloudOnlyStandInRefusesWithTheReason(t *testing.T) {
	for _, args := range [][]string{
		{"status"}, {"enable"}, {"list", "--json"}, {"show", lavaflowRunID}, {"runs", "start", lavaflowIssueID, "--json"},
	} {
		_, err := executeLavaflowCommand(t, NewCloudOnly(), args...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"lavaflow" is a cloud command`)
		assert.Contains(t, err.Error(), "LavaFlow runs on Volcano's cloud service, which local development doesn't run")
		assert.Contains(t, err.Error(), "volcano cloud lavaflow")
	}

	out, err := executeLavaflowCommand(t, NewCloudOnly(), "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "this command is cloud-only")
}

// The description is the approved AI disclosure, word for word, and opens the
// help of the command group and of enable.
func TestTheHelpOpensWithTheAIDisclosure(t *testing.T) {
	assert.Equal(t, "LavaFlow uses AI to diagnose this project's errors from their logs and deployed source, "+
		"and opens fix pull requests in your connected repository.", clilavaflow.OnDescription)

	_, deps := newLavaflowServer(t)
	for _, args := range [][]string{{"--help"}, {"enable", "--help"}} {
		out, err := executeLavaflowCommand(t, New(deps), args...)
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(out, clilavaflow.OnDescription), "%q opens with %q", args, out)
		assertLavaFlowWording(t, out)
	}
}

// lavaflowNotAvailable is what every command says where LavaFlow isn't open
// to a project without LavaFlow records or its owner yet.
const lavaflowNotAvailable = "LavaFlow isn't available for this project yet"

// lavaflowPlanRequired is what every command says where the owner's plan
// doesn't include LavaFlow and the project has no LavaFlow records.
const lavaflowPlanRequired = "LavaFlow needs a plan that includes it, and the project owner's plan doesn't"

// A project without LavaFlow records gets feature_unavailable from every
// route while LavaFlow isn't open to it, and plan_required while the owner's
// plan doesn't include LavaFlow; a platform release older than LavaFlow has
// none of its routes. Every command says which rather than report a missing
// project or run, and exits nonzero with the refusal as the cause: 4 for a
// 404, 5 for the plan's 403.
func TestEveryCommandSaysWhenLavaFlowIsNotAvailable(t *testing.T) {
	for name, tc := range map[string]struct {
		off    func(*lavaflowServer)
		want   string
		status int
	}{
		"not open": {func(s *lavaflowServer) {
			s.capability = "unavailable"
			s.refuse = http.StatusNotFound
			s.refusal = map[string]any{"error": "LavaFlow is not available", "code": "feature_unavailable"}
		}, lavaflowNotAvailable, http.StatusNotFound},
		"no plan": {func(s *lavaflowServer) {
			s.capability = "unavailable"
			s.refuse = http.StatusForbidden
			s.refusal = map[string]any{"error": "LavaFlow needs a plan that includes it", "code": "plan_required"}
		}, lavaflowPlanRequired, http.StatusForbidden},
		"no routes": {func(s *lavaflowServer) { s.routesMissing = true }, "LavaFlow isn't available on this Volcano API", http.StatusNotFound},
	} {
		for _, args := range lavaflowCommandArgs() {
			fake, deps := newLavaflowServer(t)
			fake.set(tc.off)

			_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), append(args, "--json")...)
			require.Error(t, err, "%s %q", name, args)
			assert.Equal(t, tc.want, err.Error(), "%s %q", name, args)
			assert.Equal(t, tc.status, api.Status(err), "%s %q", name, args)
			assert.NotContains(t, errOut, clilavaflow.EnableNotice, "%s %q: nothing is asked", name, args)
			assert.Empty(t, fake.updates, "%s %q", name, args)
		}
	}
}

// Every LavaFlow request names the current project: in the query on a read,
// in the body on the settings save.
func TestEveryRequestNamesTheProject(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	for _, args := range lavaflowCommandArgs() {
		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.NoError(t, err, "%q", args)
	}

	fake.set(func(s *lavaflowServer) {
		assert.Len(t, s.updates, 3, "enable, disable and settings set saved")
		require.NotEmpty(t, s.projects)
		for _, project := range s.projects {
			assert.Equal(t, lavaflowProjectID, project)
		}
	})
}

// A project the caller can't use, such as another owner's, answers 404
// project not found from every route, as one that doesn't exist does. The
// API's words come through with the exit code of a missing resource.
func TestAProjectTheCallerCantUseIsNotFound(t *testing.T) {
	for _, args := range lavaflowCommandArgs() {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse = http.StatusNotFound
			s.refusal = map[string]any{"error": "project not found"}
		})

		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.Error(t, err, "%q", args)
		assert.Contains(t, err.Error(), "project not found", "%q", args)
		assert.NotContains(t, err.Error(), "isn't available", "%q", args)
		assert.Equal(t, http.StatusNotFound, api.Status(err), "%q", args)
		assert.Empty(t, fake.updates, "%q", args)
	}
}

func lavaflowCommandArgs() [][]string {
	return [][]string{
		{"status"},
		{"enable", "--yes"},
		{"disable"},
		{"settings"},
		{"settings", "set", "--autonomy", "diagnose_only"},
		{"stats"},
		{"resources"},
		{"list"},
		{"show", lavaflowRunID},
		{"runs", "start", lavaflowIssueID},
	}
}

// lavaflowCantTurnOn is what enable says, before it asks, when the project's
// capabilities say LavaFlow can't be turned on for a project with records.
const lavaflowCantTurnOn = "LavaFlow can't be turned on for this project: " +
	"it isn't open to the owner's account, or the owner's plan doesn't include it"

// Turning LavaFlow on needs it available, which the project's capabilities
// say before anything is asked or sent.
func TestEnableNeedsLavaFlowAvailable(t *testing.T) {
	for _, capability := range []string{"unavailable", "", "limited"} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.capability = capability })

		_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "enable")
		require.Error(t, err, "%q", capability)
		assert.Equal(t, lavaflowCantTurnOn, err.Error(), "%q", capability)
		assert.Equal(t, http.StatusNotFound, api.Status(err))
		assert.NotContains(t, errOut, clilavaflow.EnableNotice, "nothing is asked")
		assert.Empty(t, fake.updates)
	}
}

// A project that has LavaFlow records keeps reading them, and can turn
// LavaFlow off, while LavaFlow is unavailable for it. enable stops before it
// asks; settings set asks nothing, so the save says why: 4 while LavaFlow
// isn't open to the project, 5 for the owner's plan.
func TestAProjectWithRecordsReadsAndTurnsOffWhileUnavailable(t *testing.T) {
	for _, planOff := range []bool{false, true} {
		testAProjectWithRecordsWhileUnavailable(t, planOff)
	}
}

func testAProjectWithRecordsWhileUnavailable(t *testing.T, planOff bool) {
	t.Helper()
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.capability, s.gateOff, s.planOff = "unavailable", true, planOff
		s.settings["enabled"] = true
	})

	for _, args := range [][]string{{"settings"}, {"stats"}, {"resources"}, {"list"}, {"show", lavaflowRunID}} {
		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.NoError(t, err, "%q", args)
	}

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow isn't available for this project, so it can't be turned on or changed. "+
		"Its records stay readable, and the owner can still turn it off.")
	out, err = executeLavaflowCommand(t, New(deps), "status", "--json")
	require.NoError(t, err)
	assert.Equal(t, false, decodeJSON(t, out)["available"])

	out, err = executeLavaflowCommand(t, New(deps), "disable")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow is off")
	assert.Equal(t, map[string]any{"enabled": false}, fake.lastUpdate(t))

	_, err = executeLavaflowCommand(t, New(deps), "enable", "--yes")
	require.Error(t, err)
	assert.Equal(t, lavaflowCantTurnOn, err.Error())
	assert.Equal(t, http.StatusNotFound, api.Status(err))
	assert.Len(t, fake.updates, 1, "enable sent nothing")

	_, err = executeLavaflowCommand(t, New(deps), "settings", "set", "--system-prompt", "x")
	require.Error(t, err)
	want, status := lavaflowNotAvailable, http.StatusNotFound
	if planOff {
		want, status = lavaflowPlanRequired, http.StatusForbidden
	}
	assert.Equal(t, want, err.Error(), "plan off: %v", planOff)
	assert.Equal(t, status, api.Status(err), "plan off: %v", planOff)
	assert.Equal(t, map[string]any{"system_prompt": "x"}, fake.lastUpdate(t), "the save said why")
}

// Every command answers --json with the API's own fields and values, the
// same the web shows.
func TestEveryCommandWritesTheAPIsValuesAsJSON(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want func(map[string]any)
	}{
		{[]string{"settings"}, func(v map[string]any) {
			assert.Equal(t, defaultLavaflowSettings(), v)
		}},
		{[]string{"stats"}, func(v map[string]any) {
			assert.Equal(t, asJSON(t, lavaflowStats()), v)
		}},
		{[]string{"resources"}, func(v map[string]any) {
			assert.Len(t, v["data"], 2)
			first := v["data"].([]any)[0].(map[string]any)
			assert.Equal(t, checkoutResource(), first["resource"])
			assert.Equal(t, map[string]any{"flagged": true, "reason": "6 issues in the last 24 hours, 4 times the daily average"}, first["anomaly"])
		}},
		{[]string{"list"}, func(v map[string]any) {
			items := v["data"].([]any)
			require.Len(t, items, 2)
			assert.Equal(t, false, v["has_more"])
			assert.Equal(t, "pr_open", items[0].(map[string]any)["status"])
			assert.NotContains(t, items[0], "diff")
		}},
		{[]string{"show", lavaflowFailedID}, func(v map[string]any) {
			assert.Equal(t, map[string]any{"code": "source_unavailable", "message": lavaflowFailureMessage}, v["failure"])
		}},
	} {
		_, deps := newLavaflowServer(t)
		out, err := executeLavaflowCommand(t, New(deps), append(tc.args, "--json")...)
		require.NoError(t, err, "%q", tc.args)
		tc.want(decodeJSON(t, out))
	}
}
