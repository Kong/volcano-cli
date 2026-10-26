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

// A platform release older than LavaFlow answers its routes with a bare 404,
// which every command says as LavaFlow not being there yet rather than as a
// missing project or run. The 404 stays the cause, for the exit code.
func TestEveryCommandSaysWhenTheAPIHasNoLavaFlow(t *testing.T) {
	for _, args := range [][]string{
		{"status"},
		{"enable"},
		{"disable"},
		{"runs", "list"},
		{"runs", "get", lavaflowRunID},
		{"runs", "start", "--issue", lavaflowIssueID},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.routesMissing = true })

		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.Error(t, err, "%q", args)
		assert.Equal(t, "LavaFlow isn't available on this Volcano API yet", err.Error(), "%q", args)
		assert.Equal(t, http.StatusNotFound, api.Status(err), "%q", args)
	}
}
