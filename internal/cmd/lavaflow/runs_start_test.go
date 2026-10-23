package lavaflow

import (
	"net/http"
	"testing"
	"time"

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

// A run limit says when a run can start, and the CLI never retries on its
// own: every run counts toward the project's usage.
func TestRunsStartReportsARunLimitAndWhenToRetry(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	deps.Now = func() time.Time { return now }
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusTooManyRequests
		s.refusal = map[string]any{
			"error": "LavaFlow ran this issue 5 times in the last 24 hours. Try again after Retry-After.",
			"code":  "issue_run_limit",
		}
	})

	for header, want := range map[string]string{
		"7920":                          "Try again after 2026-10-23 14:12 UTC (in 2h12m).",
		"Fri, 23 Oct 2026 12:00:30 GMT": "Try again in a minute.",
	} {
		fake.set(func(s *lavaflowServer) {
			s.refusalHeader = http.Header{"Retry-After": {header}}
			s.started = nil
		})
		_, err := executeLavaflowCommand(t, New(deps), "runs", "start", "--issue", lavaflowIssueID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "LavaFlow ran this issue 5 times in the last 24 hours.")
		assert.Contains(t, err.Error(), want)
		assert.Equal(t, http.StatusTooManyRequests, api.Status(err))
		assert.Equal(t, "issue_run_limit", api.Code(err))
	}
}

// The server's message already splits the two cases, so it is shown as given.
func TestRunsStartShowsWhyTheRepositoryChangeStopsIt(t *testing.T) {
	const message = "The project's repository changed from acme/old since LavaFlow was turned on. " +
		"Turn LavaFlow on again to use the new repository."
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusConflict
		s.refusal = map[string]any{"error": message, "code": "repository_changed"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "runs", "start", "--issue", lavaflowIssueID)
	require.Error(t, err)
	assert.Equal(t, message, err.Error())
	assert.Equal(t, http.StatusConflict, api.Status(err), "the exit code still reports the refusal")
}
