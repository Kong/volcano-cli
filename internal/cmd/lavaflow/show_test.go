package lavaflow

import (
	"net/http"
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
		"Review: passed after 2 round(s)\nFix:\n  Pull request: #42 https://github.com/acme/shop/pull/42\n  State: open\n",
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
	assert.Contains(t, out, "  Opened: "+opened+"\n")

	fake.set(func(s *lavaflowServer) { s.runs[0]["pull_request"].(map[string]any)["opened_at"] = nil })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "  Pull request: #42 https://github.com/acme/shop/pull/42\n  State: open\n")
	assert.NotContains(t, out, "Opened:")
}

// show says where a run's merge is and, once LavaFlow holds the fix, each
// reason in the API's words, with the paths it names, and that the pull
// request is now the person's. A reason without words prints its code.
func TestShowSaysWhyLavaFlowHoldsTheFix(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.holdFix(
			holdReason("sensitive_path", "It changes authentication configuration.", "volcano/auth.yaml", "volcano/env.yaml"),
			holdReason("person_commits", ""),
		)
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Fix:\n"+
		"  Pull request: #42 https://github.com/acme/shop/pull/42\n"+
		"  State: open\n"+
		"  Opened: "+time.Date(2026, 10, 30, 9, 3, 0, 0, time.UTC).Local().Format("2006-01-02 15:04")+"\n"+
		"  Merge: held\n"+
		"    - It changes authentication configuration. (volcano/auth.yaml, volcano/env.yaml)\n"+
		"    - person commits\n"+
		"  The pull request is now yours to review and merge on GitHub.\n")
}

// Each merge status reads as words, with its reason; a stopped fix is the
// person's from then on. One the CLI doesn't know prints as given, and a run
// without a merge prints none.
func TestShowSaysWhereTheMergeIs(t *testing.T) {
	yours := "The pull request is now yours to review and merge on GitHub."
	for _, tc := range []struct {
		status, reason, want string
	}{
		{"waiting_for_checks", "", "Merge: waiting for CI\n"},
		// A paused merge keeps its status, whose checks may have finished.
		{"waiting_for_checks", "paused", "Merge: paused while merging is off\n"},
		{"queued", "paused", "Merge: paused while merging is off\n"},
		{"queued", "", "Merge: queued to merge\n"},
		{"merged", "", "Merge: merged\n"},
		{"stopped", "checks_failed", "Merge: stopped (checks failed)\n  " + yours + "\n"},
		{"stopped", "paused_too_long", "Merge: stopped (paused too long)\n  " + yours + "\n"},
		{"new_status", "", "Merge: new_status\n"},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.holdFix()
			s.runs[0]["merge"].(map[string]any)["status"] = tc.status
			if tc.reason != "" {
				s.runs[0]["merge"].(map[string]any)["reason"] = tc.reason
			}
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, tc.status)
		assert.Contains(t, out, tc.want, tc.status)
		if tc.status != "stopped" {
			assert.NotContains(t, out, yours, tc.status)
		}
	}

	_, deps := newLavaflowServer(t)
	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.NotContains(t, out, "Merge:")
}

// A run whose pull request opened past ungated workflows lists the ones the
// API names: the file, the branch or commit whose workflows run it, and the
// job and the event when the file could be read. The API names only the first
// on each branch or commit, so show gives no count that reads as a total. A
// run with none says nothing about it.
func TestShowListsTheUngatedWorkflowsAPullRequestOpenedPast(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["ungated_workflows"] = []any{
			map[string]any{"source": "main", "path": ".github/workflows/preview.yml", "job": "deploy-preview", "trigger": "pull_request_target"},
			map[string]any{"source": "4f1c2b3", "path": ".github/workflows/broken.yml"},
		}
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Ungated workflows: the pull request opened past these, the first one on each branch or commit checked\n"+
		"  - .github/workflows/preview.yml on main: job deploy-preview, runs on pull_request_target\n"+
		"  - .github/workflows/broken.yml on 4f1c2b3: couldn't be read\n")

	fake.set(func(s *lavaflowServer) { s.runs[0]["ungated_workflows"] = []any{} })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.NotContains(t, out, "Ungated workflows")
}

// A held or stopped fix whose pull request a person merged or closed keeps
// its merge line and reasons, without saying the pull request is still
// theirs to merge.
func TestShowDropsTheHandOffOnceThePullRequestCloses(t *testing.T) {
	yours := "The pull request is now yours to review and merge on GitHub."
	for _, status := range []string{"held", "stopped"} {
		for _, state := range []string{"merged", "closed"} {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.holdFix(holdReason("label", "The pull request has the needs-human-review label."))
				s.runs[0]["merge"].(map[string]any)["status"] = status
				s.runs[0]["pull_request"].(map[string]any)["state"] = state
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err, status+"/"+state)
			assert.Contains(t, out, "Merge: "+status+"\n", status+"/"+state)
			assert.NotContains(t, out, yours, status+"/"+state)
		}
	}
}

// An open pull request GitHub reports as a draft shows as one, with why in
// plain words. A merged or closed one shows its state, and one opened before
// LavaFlow recorded drafts shows as open.
func TestShowSaysWhyThePullRequestIsADraft(t *testing.T) {
	for _, tc := range []struct {
		name, state   string
		draft, reason any
		want          string
	}{
		{"open_pr", "open", true, "open_pr", "State: draft (LavaFlow doesn't merge it; review it and mark it ready)\n"},
		{
			"review_unresolved", "open", true, "review_unresolved",
			"State: draft (LavaFlow's review didn't pass the fix, so LavaFlow won't merge it; decide whether to finish it)\n",
		},
		{"converted_to_draft", "open", true, "converted_to_draft", "State: draft (someone made it a draft after LavaFlow opened it)\n"},
		{"unknown reason", "open", true, "new_reason", "State: draft (new reason)\n"},
		{"no reason", "open", true, nil, "State: draft\n"},
		{"not a draft", "open", false, nil, "State: open\n"},
		{"not recorded", "open", nil, nil, "State: open\n"},
		{"closed draft", "closed", true, "open_pr", "State: closed\n"},
		{"merged", "merged", false, nil, "State: merged\n"},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			pr := s.runs[0]["pull_request"].(map[string]any)
			pr["state"], pr["draft"], pr["draft_reason"] = tc.state, tc.draft, tc.reason
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, tc.name)
		assert.Contains(t, out, "Fix:\n  Pull request: #42 https://github.com/acme/shop/pull/42\n  "+tc.want, tc.name)
	}
}

// A merged fix says whether LavaFlow merged it. An API older than
// merged_by_lavaflow leaves it out, and the fix then says only that it merged.
func TestShowSaysWhoMergedTheFix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mergedBy any
		omit     bool
		want     string
	}{
		{"by LavaFlow", true, false, "  Merge: merged by LavaFlow\n"},
		{"by someone else", false, false, "  Merge: merged by someone else\n"},
		{"null", nil, false, "  Merge: merged\n"},
		{"missing", nil, true, "  Merge: merged\n"},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.runs[0]["status"] = "completed"
			s.runs[0]["pull_request"].(map[string]any)["state"] = "merged"
			merge := map[string]any{"status": "merged", "reason": nil, "hold_reasons": []any{}}
			if !tc.omit {
				merge["merged_by_lavaflow"] = tc.mergedBy
			}
			s.runs[0]["merge"] = merge
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, tc.name)
		assert.Contains(t, out, "  State: merged\n", tc.name)
		assert.Contains(t, out, tc.want, tc.name)
		assert.NotContains(t, out, "yours to review", tc.name)
	}
}

// A completed run with no pull request says why in plain words, with what to
// do next, for every code the API documents, and for diagnose_only once the
// API sends it. An unknown code prints as given.
func TestShowSaysWhyARunOpenedNoPullRequest(t *testing.T) {
	const none = "No fix pull request was opened"
	for code, want := range map[string]string{
		"not_fixable": none + ": the diagnosis found no change in the code that fixes the error, " +
			"for example because the cause is outside it. Read the diagnosis below.",
		"low_confidence": none + ": the diagnosis isn't confident enough to propose a fix. Read the diagnosis below.",
		"no_patch":       none + ": LavaFlow proposed no change. Read the diagnosis below.",
		"possible_credential": none + ": the fix or its description held what looks like a credential or a project secret, " +
			"so LavaFlow pushed nothing. Apply the fix yourself from the diagnosis below.",
		"possible_personal_data": none + ": the fix adds an email address the repository lacks, and the repository isn't private, " +
			"so LavaFlow pushed nothing. Apply the fix yourself.",
		"ungated_workflows": none + ": a workflow could run the fix with the repository's secrets before a person approves it. " +
			"Gate that workflow on a label a person adds, or have the project's owner allow ungated workflows.",
		"pull_request_open": none + ": another LavaFlow pull request for this issue is open. Review or close it first.",
		"source_unavailable": none + ": LavaFlow couldn't read the repository at the deployed commit. " +
			"Check the project's repository connection and that the commit exists.",
		"permission_missing": none + ": the Volcano GitHub App, or the project owner's GitHub account, lacks a permission LavaFlow needs. " +
			"Grant the App's requested permissions, and make sure the owner can push to the repository.",
		"repository_changed": none + ": the project no longer deploys from the repository the run read. A new run uses the current one.",
		"publish_failed":     none + ": opening the pull request failed. A new run tries again.",
		"diagnose_only":      "Diagnosis only: this run wasn't set to open a fix.",
		"new_code\x1b[2J":    none + ", for a reason this version of the CLI doesn't know: new_code\\x1b[2J.",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.openNoPullRequest(code) })

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, code)
		assert.Contains(t, out, "Fix:\n  "+want+"\n", code)
		assert.NotContains(t, out, "Pull request:", code)
		assert.NotContains(t, out, "\x1b", code)
		assertLavaFlowWording(t, out)
	}
}

// A reason that points to the diagnosis leaves the pointer out when show
// prints no diagnosis.
func TestShowPointsToADiagnosisOnlyWhenItPrintsOne(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.openNoPullRequest("possible_credential")
		s.runs[0]["diagnosis"] = nil
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "so LavaFlow pushed nothing. Apply the fix yourself.\n")
	assert.NotContains(t, out, "below")
}

// A completed run with no pull request and no reason claims none: it may
// have stopped short for any reason, an unreadable source among them. It
// points to the diagnosis when show prints one.
func TestShowClaimsNoReasonTheRunDidntRecord(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.openNoPullRequest(nil) })

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Fix:\n  No fix pull request was opened. LavaFlow didn't record why. Read the diagnosis below.\n")
	assert.NotContains(t, out, "Diagnosis only")
	assert.Contains(t, out, "\nDiagnosis\n  Checkout reads the cart before it loads.\n")

	// An API older than pull_request_skipped leaves it out.
	fake.set(func(s *lavaflowServer) {
		delete(s.runs[0], "pull_request_skipped")
		s.runs[0]["diagnosis"] = nil
	})
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Fix:\n  No fix pull request was opened. LavaFlow didn't record why.\n")
}

// A run at work, or waiting to start, has no fix yet. One that failed or was
// cancelled says so, and a failure then says why.
func TestShowSaysARunWithoutAPullRequestHasNoFix(t *testing.T) {
	for status, want := range map[string]string{
		"queued":     "No fix yet: LavaFlow is still working on this run.",
		"diagnosing": "No fix yet: LavaFlow is still working on this run.",
		"fixing":     "No fix yet: LavaFlow is still working on this run.",
		"reviewing":  "No fix yet: LavaFlow is still working on this run.",
		"cancelled":  "No fix pull request was opened: the run was cancelled.",
		"new_stage":  "No fix pull request was opened.",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.openNoPullRequest(nil)
			s.runs[0]["status"] = status
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, status)
		assert.Contains(t, out, "Fix:\n  "+want+"\n", status)
	}

	_, deps := newLavaflowServer(t)
	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowFailedID)
	require.NoError(t, err)
	assert.Contains(t, out, "Fix:\n  No fix pull request was opened: the run failed.\nWarning: "+lavaflowFailureMessage+"\n")
}

// The run says which commit it read the code at, and where that commit came
// from in plain words. A run that read none, or a server that doesn't send
// the field, prints no source; one from before Volcano kept the origin, or
// with an origin the CLI doesn't know, prints the commit alone.
func TestShowSaysWhichCommitTheRunRead(t *testing.T) {
	for origin, want := range map[any]string{
		"push":                       "Source: commit 4f1c2b3 (deployed from a push)\n",
		"client_reported":            "Source: commit 4f1c2b3 (reported by the deploy; deployed files match)\n",
		"client_reported_repository": "Source: commit 4f1c2b3 (reported by the deploy; found on the production branch)\n",
		"repository":                 "Source: commit 4f1c2b3 (production branch; may not be deployed)\n",
		"mirrored\x1b[2J":            "Source: commit 4f1c2b3\n",
		nil:                          "Source: commit 4f1c2b3\n",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.runs[0]["source_commit"] = map[string]any{"sha": lavaflowSourceSHA, "origin": origin}
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, origin)
		assert.Contains(t, out, "Issue: "+lavaflowIssueID+"\n"+want, origin)

		out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
		require.NoError(t, err, origin)
		assert.Equal(t, map[string]any{"sha": lavaflowSourceSHA, "origin": origin}, decodeJSON(t, out)["source_commit"], origin)
	}

	fake, deps := newLavaflowServer(t)
	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowFailedID)
	require.NoError(t, err)
	assert.NotContains(t, out, "Source", "null")
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowFailedID, "--json")
	require.NoError(t, err)
	source, sent := decodeJSON(t, out)["source_commit"]
	assert.True(t, sent)
	assert.Nil(t, source)

	fake.set(func(s *lavaflowServer) { delete(s.runs[0], "source_commit") })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.NotContains(t, out, "Source", "absent")
}

// show --json and list --json print the API's body as it came: every field
// the API documents for a run, and one newer than the CLI's contract, which
// re-encoding the CLI's own type would drop.
func TestRunJSONKeepsEveryFieldTheAPISends(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		pr := s.runs[0]["pull_request"].(map[string]any)
		pr["state"], pr["draft"], pr["draft_reason"], pr["deployed_at"] = "merged", false, nil, "2026-10-30T09:20:00Z"
		s.runs[0]["status"] = "completed"
		s.runs[0]["merge"] = map[string]any{"status": "merged", "reason": nil, "hold_reasons": []any{}, "merged_by_lavaflow": true}
		s.runs[0]["failure"] = nil
		s.runs[0]["ungated_workflows"] = []any{}
		s.runs[1]["pull_request_skipped"] = "source_unavailable"
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
	require.NoError(t, err)
	fake.mu.Lock()
	want := asJSON(t, fake.runs[0])
	fake.mu.Unlock()
	assert.Equal(t, want, decodeJSON(t, out))
	assert.NotContains(t, out, "Fix:")

	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowFailedID, "--json")
	require.NoError(t, err)
	assert.Equal(t, "source_unavailable", decodeJSON(t, out)["pull_request_skipped"])

	out, err = executeLavaflowCommand(t, New(deps), "list", "--json")
	require.NoError(t, err)
	items := decodeJSON(t, out)["data"].([]any)
	require.Len(t, items, 2)
	first, second := items[0].(map[string]any), items[1].(map[string]any)
	for _, field := range []string{
		"id", "project_id", "status", "resource", "issue_id", "title", "created_at", "finished_at",
		"pull_request", "merge", "pull_request_skipped",
	} {
		assert.Contains(t, first, field)
	}
	assert.Equal(t, "2026-10-30T09:20:00Z", first["pull_request"].(map[string]any)["deployed_at"])
	assert.Equal(t, true, first["merge"].(map[string]any)["merged_by_lavaflow"])
	assert.Nil(t, first["pull_request_skipped"])
	assert.Equal(t, "source_unavailable", second["pull_request_skipped"])
}
