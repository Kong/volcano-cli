package lavaflow

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
)

func TestRunsStartQueuesARunForTheIssue(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "runs", "start", "--issue", lavaflowIssueID)
	require.NoError(t, err)
	fake.set(func(s *lavaflowServer) { assert.Equal(t, []string{lavaflowIssueID}, s.started) })
	assert.Contains(t, out, "LavaFlow run "+lavaflowRunID+" queued")
	assert.Contains(t, out, "Follow it: volcano cloud lavaflow runs get "+lavaflowRunID)
}

func TestRunsStartNeedsAValidIssue(t *testing.T) {
	_, deps := newLavaflowServer(t)

	_, err := executeLavaflowCommand(t, New(deps), "runs", "start")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `required flag(s) "issue" not set`)

	_, err = executeLavaflowCommand(t, New(deps), "runs", "start", "--issue", "nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid issue ID "nope"`)
}

func TestRunsStartReportsAConflict(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusConflict
		s.refusal = map[string]any{"error": "the issue already has a queued or running run"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "runs", "start", "--issue", lavaflowIssueID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the issue already has a queued or running run")
	assert.Equal(t, http.StatusConflict, api.Status(err))
}
