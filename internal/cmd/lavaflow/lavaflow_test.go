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
	for _, args := range [][]string{{"status"}, {"enable"}, {"runs", "list", "--json"}} {
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

// An API where LavaFlow is not on answers with a 404: a bare one from a
// platform release older than LavaFlow, which has none of its routes, or the
// API's own feature_unavailable refusal where its release gate leaves the
// account out. Every command says which rather than report a missing project
// or run, and the 404 stays the cause, for a nonzero exit code.
func TestEveryCommandSaysWhenLavaFlowIsNotAvailable(t *testing.T) {
	for name, tc := range map[string]struct {
		off  func(*lavaflowServer)
		want string
	}{
		"no routes": {func(s *lavaflowServer) { s.routesMissing = true }, "LavaFlow isn't available on this Volcano API"},
		"turned off for the account": {func(s *lavaflowServer) {
			s.refuse = http.StatusNotFound
			s.refusal = map[string]any{"error": "LavaFlow is not available", "code": "feature_unavailable"}
		}, "LavaFlow isn't available to your account yet"},
	} {
		for _, args := range [][]string{
			{"status"},
			{"enable"},
			{"disable"},
			{"runs", "list"},
			{"runs", "get", lavaflowRunID},
			{"runs", "start", "--issue", lavaflowIssueID},
		} {
			fake, deps := newLavaflowServer(t)
			fake.set(tc.off)

			_, err := executeLavaflowCommand(t, New(deps), args...)
			require.Error(t, err, "%s %q", name, args)
			assert.Equal(t, tc.want, err.Error(), "%s %q", name, args)
			assert.Equal(t, http.StatusNotFound, api.Status(err), "%s %q", name, args)
		}
	}
}
