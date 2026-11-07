package lavaflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatsShowsEachDayAndTheTotals(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "stats")
	require.NoError(t, err)
	assert.Equal(t, "30", fake.lastQuery(t, "/stats").Get("days"), "the default window")
	assert.Regexp(t, `2026-10-30\s+5\s+3\n`, out)
	assert.Contains(t, out, "\n7 issues and 4 fix pull requests opened from 2026-10-28 to 2026-10-30\n")
	assertLavaFlowWording(t, out)
}

// A count of one reads singular, and a one-day window names its day once.
func TestStatsSaysOneDayAndOneOfEach(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.stats = map[string]any{
			"project_id": lavaflowProjectID, "window_start": "2026-11-07", "window_end": "2026-11-07",
			"days":   []any{map[string]any{"date": "2026-11-07", "issues_opened": 1, "pull_requests_opened": 1}},
			"totals": map[string]any{"issues_opened": 1, "pull_requests_opened": 1},
		}
	})

	out, err := executeLavaflowCommand(t, New(deps), "stats", "--days", "1")
	require.NoError(t, err)
	assert.Contains(t, out, "\n1 issue and 1 fix pull request opened on 2026-11-07\n")
	assert.NotContains(t, out, "(s)")
}

func TestStatsAndResourcesTakeOneToNinetyDays(t *testing.T) {
	for _, command := range []string{"stats", "resources"} {
		for _, days := range []string{"1", "90"} {
			fake, deps := newLavaflowServer(t)
			_, err := executeLavaflowCommand(t, New(deps), command, "--days", days)
			require.NoError(t, err, "%s %s", command, days)
			assert.Equal(t, days, fake.lastQuery(t, "/"+command).Get("days"))
		}
		for _, days := range []string{"0", "91"} {
			fake, deps := newLavaflowServer(t)
			_, err := executeLavaflowCommand(t, New(deps), command, "--days", days)
			require.EqualError(t, err, "--days must be between 1 and 90", "%s %s", command, days)
			assert.Empty(t, fake.queries["/"+command])
		}
	}
}
