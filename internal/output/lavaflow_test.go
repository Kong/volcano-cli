package output

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func TestLavaflowDeliveryStatus(t *testing.T) {
	t.Parallel()

	nextAction := "Check the repository connection."
	reason := "source_unavailable"
	for _, tc := range []struct {
		state  apiclient.LavaflowDeliveryState
		label  string
		action *string
	}{
		{apiclient.LavaflowDeliveryStateWaitingForPrerequisite, "Waiting", nil},
		{apiclient.LavaflowDeliveryStateTerminalMiss, "Undelivered", &nextAction},
		{apiclient.LavaflowDeliveryStateStopped, "Stopped", &nextAction},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			t.Parallel()
			delivery := &apiclient.LavaflowDelivery{State: tc.state, Reason: &reason, NextAction: tc.action}
			run := &apiclient.LavaflowRun{Status: apiclient.LavaflowRunStatusCompleted, Delivery: delivery}

			require.Equal(t, tc.label, lavaflowDeliveryCell(nil, delivery))
			var out bytes.Buffer
			lavaflowFix(&out, false, run)
			require.Contains(t, out.String(), "Delivery: "+tc.label+" ("+reason+")")
			if tc.action != nil {
				require.Contains(t, out.String(), "Next action: "+*tc.action)
			}
		})
	}
}

func TestLavaflowFixPrintsCoveringPullRequestState(t *testing.T) {
	t.Parallel()

	deployed := time.Date(2026, time.November, 19, 18, 15, 0, 0, time.UTC)
	run := &apiclient.LavaflowRun{Delivery: &apiclient.LavaflowDelivery{
		Coverage: &apiclient.LavaflowDeliveryCoverage{
			Number: 24, Url: "https://github.com/example/project/pull/24",
			State: apiclient.LavaflowDeliveryCoverageStateMerged, DeployedAt: &deployed,
		},
	}}
	var out bytes.Buffer
	lavaflowFix(&out, false, run)
	require.Equal(t, "Fix:\n  Covering pull request: #24 https://github.com/example/project/pull/24\n  State: merged\n", out.String())
}
