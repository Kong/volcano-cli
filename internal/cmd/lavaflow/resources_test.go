package lavaflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourcesFlagsASuddenJumpInIssues(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "resources", "--days", "7")
	require.NoError(t, err)
	assert.Regexp(t, `function\s+checkout\s+\S+ \S+\s+6\s+3\s+issue rate jumped\n`, out)
	assert.Regexp(t, `durable function\s+nightly-report\s+never\s+0\s+0`, out)
	assert.Contains(t, out, "2 resource(s), last 7 day(s)")
	assert.Contains(t, out, "checkout: 6 issues in the last 24 hours, 4 times the daily average")
	assertLavaFlowWording(t, out)
}

func TestResourcesSaysWhenTheProjectHasNone(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.resources = []map[string]any{} })

	out, err := executeLavaflowCommand(t, New(deps), "resources")
	require.NoError(t, err)
	assert.Equal(t, "No functions, frontends or durable functions\n", out)
}
