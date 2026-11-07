package lavaflow

import (
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// list shows each run newest first: its status, what came of it, the
// resource, when it was queued in UTC, and its whole title, last, so titles
// that start alike still read apart.
func TestListShowsEachRunNewestFirst(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Equal(t, url.Values{"limit": {"25"}}, fake.lastQuery(t, "/runs"))
	assert.Equal(t, ""+
		"ID                                    Status                           Outcome                         Resource                  Queued             Title\n"+
		strings.Repeat("-", 160)+"\n"+
		"44444444-4444-4444-8444-444444444444  PR open                          #42 open                        checkout                  2026-10-30 09:00Z  TypeError: Cannot read properties of undefined (reading <str>)\n"+
		"55555555-5555-4555-8555-555555555555  Failed                           -                               checkout                  2026-10-29 09:00Z  -\n"+
		"\n"+
		"Showing 2 run(s)\n", out)
	assertLavaFlowWording(t, out)
}

// Two titles that start alike both print whole.
func TestListKeepsTitlesThatStartAlikeApart(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["title"] = "Downgraded accounts stuck as Pro after the billing webhook fails"
		s.runs[1]["title"] = "Downgraded accounts stuck as Pro when the worker retries a cancelled plan"
	})

	out, err := executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Contains(t, out, "  Downgraded accounts stuck as Pro after the billing webhook fails\n")
	assert.Contains(t, out, "  Downgraded accounts stuck as Pro when the worker retries a cancelled plan\n")
	assert.NotContains(t, out, "...")
}

// The Outcome column says what came of each finished run: its pull request
// and that request's state, or why it opened none, for every reason the API
// documents. A reason the CLI doesn't know prints as given.
func TestListSaysWhatCameOfEachRun(t *testing.T) {
	for reason, want := range map[any]string{
		"diagnose_only":            "Diagnosed only",
		"not_fixable":              "No PR: no fix in the code",
		"low_confidence":           "No PR: not confident",
		"no_patch":                 "No PR: no change found",
		"possible_credential":      "No PR: possible credential",
		"possible_personal_data":   "No PR: adds an email address",
		"ungated_workflows":        "No PR: ungated workflows",
		"convention_unsatisfiable": "No PR: commit convention",
		"pull_request_open":        "No PR: another was open",
		"source_unavailable":       "No PR: couldn't read source",
		"permission_missing":       "No PR: permission missing",
		"repository_changed":       "No PR: repository changed",
		"publish_failed":           "No PR: opening it failed",
		"lineage_cap":              "No PR: follow-up limit",
		"lineage_held":             "No PR: earlier fix held",
		"review_unresolved":        "No PR: review didn't pass",
		"duplicate_in_repository":  "No PR: another project has it",
		"security_finding":         "No PR: security finding",
		"new_reason":               "No PR: new_reason",
		nil:                        "-",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.openNoPullRequest(reason) })

		out, err := executeLavaflowCommand(t, New(deps), "list")
		require.NoError(t, err, reason)
		assert.Regexp(t, lavaflowRunID+`  Finished\s+`+regexp.QuoteMeta(want)+`\s+checkout  `, out, reason)
	}

	for state, want := range map[string]string{"merged": "#42 merged", "closed": "#42 closed"} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.runs[0]["status"] = "completed"
			s.runs[0]["pull_request"].(map[string]any)["state"] = state
		})

		out, err := executeLavaflowCommand(t, New(deps), "list")
		require.NoError(t, err, state)
		assert.Regexp(t, lavaflowRunID+`  Finished\s+`+want+`\s+checkout  `, out, state)
	}
}

// The filters reach the API, and the next page keeps them.
func TestListFiltersAndPages(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.nextCursor = "next-page" })

	out, err := executeLavaflowCommand(t, New(deps), "list", "--status", "failed", "--resource", lavaflowResourceID,
		"--limit", "2", "--cursor", "this-page")
	require.NoError(t, err)
	assert.Equal(t, url.Values{
		"status": {"failed"}, "resource_id": {lavaflowResourceID}, "limit": {"2"}, "cursor": {"this-page"},
	}, fake.lastQuery(t, "/runs"))
	assert.Contains(t, out, "volcano cloud lavaflow list --limit 2 --cursor next-page --status failed --resource "+lavaflowResourceID)
}

func TestListRefusesABadFilterBeforeSending(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--resource", "checkout"}, `invalid --resource "checkout": use a resource ID from 'volcano cloud lavaflow resources --json'`},
		{[]string{"--limit", "0"}, "--limit must be between 1 and 100"},
		{[]string{"--limit", "101"}, "--limit must be between 1 and 100"},
		{[]string{"--status", "succeeded"}, `invalid --status "succeeded": use one of queued, diagnosing, fixing, ` +
			"reviewing, pr_open, completed, failed, cancelled"},
	} {
		fake, deps := newLavaflowServer(t)

		_, err := executeLavaflowCommand(t, New(deps), append([]string{"list"}, tc.args...)...)
		require.EqualError(t, err, tc.want, "%q", tc.args)
		assert.Empty(t, fake.queries["/runs"])
	}
}

func TestListSaysWhenThereAreNoRuns(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs = nil })

	out, err := executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Equal(t, "No runs\n", out)
}

// A running run says which stage it is in, in list and show alike, and a
// status the CLI doesn't know prints as the API gives it.
func TestARunsStatusSaysWhatItIsDoing(t *testing.T) {
	for status, want := range map[string]string{
		"queued":       "Queued: waiting to start",
		"diagnosing":   "Running: finding the root cause",
		"fixing":       "Running: writing the fix",
		"reviewing":    "Running: reviewing the fix",
		"pr_open":      "PR open",
		"completed":    "Finished",
		"failed":       "Failed",
		"cancelled":    "Cancelled",
		"waiting_room": "waiting_room",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.runs[0]["status"] = status })

		out, err := executeLavaflowCommand(t, New(deps), "list")
		require.NoError(t, err, status)
		assert.Contains(t, out, lavaflowRunID+"  "+want+" ", status)

		out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, status)
		assert.Contains(t, out, "Status: "+want+"\n", status)
	}
}

// A run whose fix LavaFlow held or stopped says so in the list; one still on
// its way to merging shows its run status.
func TestListMarksAFixLeftToYou(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.holdFix(holdReason("label", "The pull request has the needs-human-review label."))
	})

	out, err := executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Contains(t, out, "Held: yours to merge")

	fake.set(func(s *lavaflowServer) { s.runs[0]["merge"].(map[string]any)["status"] = "stopped" })
	out, err = executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Contains(t, out, "Stopped: yours to merge")

	fake.set(func(s *lavaflowServer) { s.runs[0]["merge"].(map[string]any)["status"] = "waiting_for_checks" })
	out, err = executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.NotContains(t, out, "yours to merge")
}

// Once a person merges or closes a held or stopped fix's pull request, the
// list shows the run's own status: nothing is left for them to merge.
func TestListStopsMarkingAFixOnceItsPullRequestCloses(t *testing.T) {
	for _, status := range []string{"held", "stopped"} {
		for _, state := range []string{"merged", "closed"} {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.holdFix(holdReason("label", "The pull request has the needs-human-review label."))
				s.runs[0]["merge"].(map[string]any)["status"] = status
				s.runs[0]["status"] = "completed"
				s.runs[0]["pull_request"].(map[string]any)["state"] = state
			})

			out, err := executeLavaflowCommand(t, New(deps), "list")
			require.NoError(t, err, status+"/"+state)
			assert.NotContains(t, out, "yours to merge", status+"/"+state)
			assert.Contains(t, out, lavaflowRunID+"  Finished ", status+"/"+state)
		}
	}
}
