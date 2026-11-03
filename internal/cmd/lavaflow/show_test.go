package lavaflow

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
)

func TestShowSaysWhatTheRunFoundAndFixed(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	for _, want := range []string{
		"Status: PR open",
		"Resource: function checkout",
		"Title: " + lavaflowIssueTitle,
		"Pull request: #42 (open) https://github.com/acme/shop/pull/42",
		"Review: passed after 2 round(s)",
		"Diagnosis\n  Checkout reads the cart before it loads.\n",
		"Root cause\n  handleCheckout dereferences cart without a guard.\n",
		"Evidence\n  - The error starts with the 10:02 deploy.\n",
		"2026-10-30T08:59:00Z ERROR TypeError: Cannot read properties of undefined (reading 'items')",
		"Diff\n  --- a/functions/checkout.ts\n",
	} {
		assert.Contains(t, out, want)
	}
	assertLavaFlowWording(t, out)
}

// A failed run says why in the API's fixed words for its code.
func TestShowPrintsTheFailureAsTheAPIWordsIt(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowFailedID)
	require.NoError(t, err)
	assert.Contains(t, out, "Status: failed")
	assert.Contains(t, out, lavaflowFailureMessage)
	assert.NotContains(t, out, "source_unavailable")
}

// Run text prints as plain text: a control character in a log line, such as
// a color, a terminal link or a carriage return, prints as a visible escape,
// and --json keeps the text as a JSON string.
func TestShowPrintsRunTextAsPlainText(t *testing.T) {
	line := "\x1b[31mred\x1b[0m \x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\ done\rover\u009b2J\x7f\x07"
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		diagnosis := s.runs[0]["diagnosis"].(map[string]any)
		diagnosis["log_lines"] = []any{map[string]any{"timestamp": "2026-10-30T08:59:00Z", "level": "error", "message": line}}
		diagnosis["summary"] = "**Bold** _claim_ with \x1b[2J\nsecond line"
		s.runs[0]["pull_request"].(map[string]any)["state"] = "open\x1b[8m"
		s.runs[0]["review"].(map[string]any)["verdict"] = "passed\x1b]0;title\x07"
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, `\x1b[31mred\x1b[0m \x1b]8;;https://example.com\x1b\link\x1b]8;;\x1b\ done\x0dover\u009b2J\x7f\x07`)
	assert.Contains(t, out, "Diagnosis\n  **Bold** _claim_ with \\x1b[2J\n  second line\n", "Markdown prints as written")
	for _, raw := range []string{"\x1b", "\r", "\x07", "\x7f", "\u009b"} {
		assert.NotContains(t, out, raw)
	}

	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
	require.NoError(t, err)
	for _, raw := range []string{"\x1b", "\r", "\x07", "\x7f", "\u009b"} {
		assert.NotContains(t, out, raw)
	}
	logLines := decodeJSON(t, out)["diagnosis"].(map[string]any)["log_lines"].([]any)
	assert.Equal(t, line, logLines[0].(map[string]any)["message"], "the JSON string decodes to the text as the API sent it")
}

// The run's log lines need the permission to read the project's logs; the
// API's refusal comes through with its status.
func TestShowPassesTheAPIsRefusalThrough(t *testing.T) {
	for status, body := range map[int]map[string]any{
		http.StatusForbidden: {"error": "you can't read this project's logs", "code": "forbidden"},
		http.StatusNotFound:  {"error": "run not found"},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.refuse, s.refusal = status, body })

		_, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get LavaFlow run "+lavaflowRunID)
		assert.Contains(t, err.Error(), body["error"])
		assert.NotContains(t, err.Error(), "isn't available")
		assert.Equal(t, status, api.Status(err))
	}
}

func TestShowRefusesARunIDThatIsntOne(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	_, err := executeLavaflowCommand(t, New(deps), "show", "latest")
	require.EqualError(t, err, `invalid run ID "latest": use an ID from 'volcano cloud lavaflow list'`)
	assert.Empty(t, fake.queries)
}

// show says when the run opened its pull request, and leaves the line out
// for a pull request opened before LavaFlow recorded the time.
func TestShowSaysWhenThePullRequestOpened(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	// The CLI prints times in the machine's zone, so the expected line is
	// built the same way rather than pinned to one zone.
	opened := time.Date(2026, 10, 30, 9, 3, 0, 0, time.UTC).Local().Format("2006-01-02 15:04")
	assert.Contains(t, out, "Pull request opened: "+opened+"\n")

	fake.set(func(s *lavaflowServer) { s.runs[0]["pull_request"].(map[string]any)["opened_at"] = nil })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Pull request: #42 (open) https://github.com/acme/shop/pull/42")
	assert.NotContains(t, out, "Pull request opened")
}

// A run whose merge waits for a person says why, one line per reason: the
// API's question as given, else the approved copy for its code; a reason with
// neither says only that it waits, once. It names the command that approves.
func TestShowSaysWhyAMergeWaits(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.waitForApproval(
			approvalReason("sensitive_path", "Merge a change to the auth configuration?", "volcano/auth.yaml", "volcano/roles.yaml"),
			approvalReason("automatic_run", nil),
			approvalReason("client_reported_commit", ""),
			approvalReason("required_check_skipped", nil),
			approvalReason("brand_new_reason", nil),
			approvalReason("another_new_reason", nil),
		)
		s.runs[0]["merge"].(map[string]any)["held_paths"] = []any{"volcano/auth.yaml"}
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Merge: waiting for your approval\n"+
		"  - Merge a change to the auth configuration? (volcano/auth.yaml, volcano/roles.yaml)\n"+
		"  - LavaFlow started this fix from your logs on its own. Approving runs CI and merges it.\n"+
		"  - This fix is built on a commit the CLI reported, which Volcano can't verify. Approving runs CI and merges it.\n"+
		"  - A required check was skipped. Approving merges it without that check.\n"+
		"  - Waiting for your approval\n"+
		"Held paths approved: volcano/auth.yaml\n")
	assert.Equal(t, 1, strings.Count(out, "  - Waiting for your approval"), "an unknown reason says so once")
	assert.Contains(t, out, "Approve the merge with 'volcano cloud lavaflow approve "+lavaflowRunID+
		"', or close the pull request to stop it.")

	// can_decide false: the API would refuse this caller's approval.
	fake.set(func(s *lavaflowServer) { s.runs[0]["merge"].(map[string]any)["can_decide"] = false })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Merge: waiting for your approval\n")
	assert.NotContains(t, out, "Approve the merge")
}

// A merge past its approval says where it is, and why it stopped.
func TestShowSaysWhereAMergeIs(t *testing.T) {
	for _, tc := range []struct{ status, reason, want string }{
		{"waiting_for_checks", "", "Merge: waiting for CI\n"},
		{"queued", "", "Merge: queued to merge\n"},
		{"merged", "", "Merge: merged\n"},
		{"stopped", "checks_failed", "Merge: stopped (checks failed)\n"},
		{"waiting_for_approval", "paused", "Merge: waiting for your approval (paused)\n"},
		{"brand_new", "", "Merge: brand_new\n"},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.waitForApproval()
			merge := s.runs[0]["merge"].(map[string]any)
			merge["status"] = tc.status
			if tc.reason != "" {
				merge["reason"] = tc.reason
			}
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, tc.status)
		assert.Contains(t, out, tc.want, tc.status)
		if tc.status != "waiting_for_approval" {
			assert.NotContains(t, out, "Approve the merge", tc.status)
			assert.NotContains(t, out, tc.want+"  - ", tc.status)
		}
	}
}

// A run that doesn't merge says nothing about merging.
func TestShowSaysNothingAboutMergingWithoutAMerge(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.NotContains(t, out, "Merge:")
	assert.NotContains(t, out, "approve")
}
