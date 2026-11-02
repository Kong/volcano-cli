package lavaflow

import (
	"net/http"
	"testing"

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
	assert.Regexp(t, `Pull request opened: 2026-10-30 \d\d:03`, out)

	fake.set(func(s *lavaflowServer) { s.runs[0]["pull_request"].(map[string]any)["opened_at"] = nil })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Pull request: #42 (open) https://github.com/acme/shop/pull/42")
	assert.NotContains(t, out, "Pull request opened")
}
