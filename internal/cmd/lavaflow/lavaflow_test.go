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
	for _, args := range [][]string{{"status"}, {"enable"}, {"list", "--json"}, {"show", lavaflowRunID}} {
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

// A project without LavaFlow records gets feature_unavailable from every
// route while LavaFlow is off for its account, and a platform release older
// than LavaFlow has none of its routes. Every command says which rather than
// report a missing project or run, and exits nonzero with the 404 as the
// cause.
func TestEveryCommandSaysWhenLavaFlowIsNotAvailable(t *testing.T) {
	for name, tc := range map[string]struct {
		off  func(*lavaflowServer)
		want string
	}{
		"no records": {func(s *lavaflowServer) {
			s.capability = "unavailable"
			s.refuse = http.StatusNotFound
			s.refusal = map[string]any{"error": "LavaFlow is not available", "code": "feature_unavailable"}
		}, "LavaFlow isn't available to your account yet"},
		"no routes": {func(s *lavaflowServer) { s.routesMissing = true }, "LavaFlow isn't available on this Volcano API"},
	} {
		for _, args := range lavaflowCommandArgs() {
			fake, deps := newLavaflowServer(t)
			fake.set(tc.off)

			_, err := executeLavaflowCommand(t, New(deps), append(args, "--json")...)
			require.Error(t, err, "%s %q", name, args)
			assert.Equal(t, tc.want, err.Error(), "%s %q", name, args)
			assert.Equal(t, http.StatusNotFound, api.Status(err), "%s %q", name, args)
			assert.Empty(t, fake.updates, "%s %q", name, args)
		}
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
	}
}

// Turning LavaFlow on and changing its settings need it available, which the
// project's capabilities say before anything is asked or sent.
func TestChangesNeedLavaFlowAvailable(t *testing.T) {
	for _, capability := range []string{"unavailable", "", "limited"} {
		for _, args := range [][]string{{"enable"}, {"settings", "set", "--autonomy", "diagnose_only"}} {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.capability = capability })

			_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), args...)
			require.Error(t, err, "%q %q", capability, args)
			assert.Equal(t, "LavaFlow isn't available to your account yet", err.Error(), "%q %q", capability, args)
			assert.Equal(t, http.StatusNotFound, api.Status(err))
			assert.NotContains(t, errOut, clilavaflow.EnableNotice, "nothing is asked")
			assert.Empty(t, fake.updates)
		}
	}
}

// A project that has LavaFlow records keeps reading them, and can turn
// LavaFlow off, while LavaFlow is unavailable for its account. Turning it on
// and changing other settings exit 4.
func TestAProjectWithRecordsReadsAndTurnsOffWhileUnavailable(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.capability, s.gateOff = "unavailable", true
		s.settings["enabled"] = true
	})

	for _, args := range [][]string{{"settings"}, {"stats"}, {"resources"}, {"list"}, {"show", lavaflowRunID}} {
		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.NoError(t, err, "%q", args)
	}

	out, err := executeLavaflowCommand(t, New(deps), "status")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow isn't available to your account yet, so it can't be turned on or changed. "+
		"Its records stay readable, and the owner can still turn it off.")
	out, err = executeLavaflowCommand(t, New(deps), "status", "--json")
	require.NoError(t, err)
	assert.Equal(t, false, decodeJSON(t, out)["available"])

	out, err = executeLavaflowCommand(t, New(deps), "disable")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow is off")
	assert.Equal(t, map[string]any{"enabled": false}, fake.lastUpdate(t))

	for _, args := range [][]string{{"enable", "--yes"}, {"settings", "set", "--system-prompt", "x"}} {
		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.Error(t, err, "%q", args)
		assert.Equal(t, http.StatusNotFound, api.Status(err), "%q", args)
	}
	assert.Len(t, fake.updates, 1, "only disable reached the API")
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
			assert.Equal(t, lavaflowResourceID, first["id"])
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
