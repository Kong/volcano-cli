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
	assert.Contains(t, out, "7 issue(s) opened and 4 fix pull request(s) opened, 2026-10-28 to 2026-10-30")
	assertLavaFlowWording(t, out)
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
