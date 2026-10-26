package lavaflow

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
)

func TestCloudOnlyStandInRefusesWithTheReason(t *testing.T) {
	for _, args := range [][]string{{"status"}, {"enable"}, {"runs", "list", "--json"}} {
		_, err := executeLavaflowCommand(t, NewCloudOnly(), args...)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `"lavaflow" is a cloud command`)
		assert.Contains(t, err.Error(), "local development has no GitHub App")
		assert.Contains(t, err.Error(), "volcano cloud lavaflow")
	}

	out, err := executeLavaflowCommand(t, NewCloudOnly(), "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "this command is cloud-only")
}

// An API where LavaFlow is not on answers with a 404: a bare one from a
// platform release older than LavaFlow, which has none of its routes, or the
// API's own feature_unavailable refusal where Volcano has not turned it on.
// Every command says so rather than report a missing project or run, and the
// 404 stays the cause, for the exit code.
func TestEveryCommandSaysWhenLavaFlowIsNotAvailable(t *testing.T) {
	for name, off := range map[string]func(*lavaflowServer){
		"no routes": func(s *lavaflowServer) { s.routesMissing = true },
		"turned off": func(s *lavaflowServer) {
			s.refuse = http.StatusNotFound
			s.refusal = map[string]any{"error": "LavaFlow is not available", "code": "feature_unavailable"}
		},
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
			fake.set(off)

			_, err := executeLavaflowCommand(t, New(deps), args...)
			require.Error(t, err, "%s %q", name, args)
			assert.Equal(t, "LavaFlow isn't available on this Volcano API", err.Error(), "%s %q", name, args)
			assert.Equal(t, http.StatusNotFound, api.Status(err), "%s %q", name, args)
		}
	}
}
