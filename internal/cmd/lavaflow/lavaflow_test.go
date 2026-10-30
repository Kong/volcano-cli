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

// LavaFlow is off for a project when its capabilities say so or leave it out,
// when a platform release older than LavaFlow has none of its routes, or when
// a route answers feature_unavailable because the release gate moved after
// the capabilities were read. Every command says which rather than report a
// missing project or run, and exits nonzero with the 404 as the cause.
func TestEveryCommandSaysWhenLavaFlowIsNotAvailable(t *testing.T) {
	notYours := "LavaFlow isn't available to your account yet"
	for name, tc := range map[string]struct {
		off  func(*lavaflowServer)
		want string
	}{
		"capability unavailable": {func(s *lavaflowServer) { s.capability = "unavailable" }, notYours},
		"capability left out":    {func(s *lavaflowServer) { s.capability = "" }, notYours},
		"capability unknown":     {func(s *lavaflowServer) { s.capability = "limited" }, notYours},
		"no routes":              {func(s *lavaflowServer) { s.routesMissing = true }, "LavaFlow isn't available on this Volcano API"},
		"refused by a route": {func(s *lavaflowServer) {
			s.refuse = http.StatusNotFound
			s.refusal = map[string]any{"error": "LavaFlow is not available", "code": "feature_unavailable"}
		}, notYours},
	} {
		for _, args := range [][]string{
			{"status"},
			{"enable"},
			{"disable"},
			{"settings"},
			{"settings", "set", "--autonomy", "diagnose_only"},
			{"stats"},
			{"resources"},
			{"list"},
			{"show", lavaflowRunID},
		} {
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
			assert.Len(t, v["items"], 2)
			first := v["items"].([]any)[0].(map[string]any)
			assert.Equal(t, lavaflowResourceID, first["id"])
			assert.Equal(t, map[string]any{"flagged": true, "reason": "6 issues in the last 24 hours, 4 times the daily average"}, first["anomaly"])
		}},
		{[]string{"list"}, func(v map[string]any) {
			items := v["items"].([]any)
			require.Len(t, items, 2)
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
