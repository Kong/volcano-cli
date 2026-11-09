package lavaflow

import (
	"net/http"
	"strings"
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
		"Review: passed after 2 rounds\n" +
			"Checks: " + lavaflowChecksMessage + "\n" +
			"Fix:\n  Pull request: #42 https://github.com/acme/shop/pull/42\n  State: open\n",
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

// A failed run says why in the API's fixed words for its code, on the Fix
// line, so the cause reads as the run's, not as a warning about now.
func TestShowPrintsTheFailureAsTheAPIWordsIt(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowFailedID)
	require.NoError(t, err)
	assert.Contains(t, out, "Status: Failed\n")
	assert.Contains(t, out, "Fix:\n  No fix pull request was opened: the run failed. "+lavaflowFailureMessage+"\n")
	assert.NotContains(t, out, "Warning:")
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

// A run the project doesn't have says so in the API's words, without the
// HTTP status, and names the command that lists the project's runs.
func TestShowSaysWhereToFindARunIDForARunNotFound(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refusal = http.StatusNotFound, map[string]any{"error": "LavaFlow run not found"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.EqualError(t, err, "failed to get LavaFlow run "+lavaflowRunID+
		": LavaFlow run not found. List the project's runs with 'volcano cloud lavaflow list'")
	assert.Equal(t, http.StatusNotFound, api.Status(err))
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
	// Times print in UTC, marked Z, whatever the machine's zone.
	assert.Contains(t, out, "  Opened: 2026-10-30 09:03Z\n")

	fake.set(func(s *lavaflowServer) { s.runs[0]["pull_request"].(map[string]any)["opened_at"] = nil })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "  Pull request: #42 https://github.com/acme/shop/pull/42\n  State: open\n")
	assert.NotContains(t, out, "Opened:")
}

// show says where a run's merge is and, once LavaFlow holds the fix, each
// reason in the API's words, with the paths it names, and that the pull
// request is now the person's. A reason without words says what its code
// means.
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
		"  Opened: 2026-10-30 09:03Z\n"+
		"  Merge: held\n"+
		"    - It changes authentication configuration. (volcano/auth.yaml, volcano/env.yaml)\n"+
		"    - Someone other than LavaFlow pushed to the fix's branch.\n"+
		"  The pull request is now yours to review and merge on GitHub.\n")
	assert.NotContains(t, out, "person_commits")
}

// A question that already names each path it holds the fix for prints
// without the paths again; one that names only some lists the rest.
func TestShowListsTheHeldPathsOnlyWhenTheQuestionDoesnt(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.holdFix(
			holdReason("path", "Changes db/migrations/0042_orders.sql, which always needs a person.", "db/migrations/0042_orders.sql"),
			holdReason("sensitive_path", "Changes volcano/auth.js, which touches authentication or authorization.",
				"volcano/auth.js", "volcano/session.js"),
			holdReason("path", "Changes src/cart.go, which needs review.", "src/cart.go", "cart.go"),
			holdReason("path", "Changes src/settings.yaml.backup, which needs review.", "src/settings.yaml"),
		)
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "  Merge: held\n"+
		"    - Changes db/migrations/0042_orders.sql, which always needs a person.\n"+
		"    - Changes volcano/auth.js, which touches authentication or authorization. (also volcano/session.js)\n"+
		"    - Changes src/cart.go, which needs review. (also cart.go)\n"+
		"    - Changes src/settings.yaml.backup, which needs review. (src/settings.yaml)\n")
}

// Each hold code the API documents reads as words when the API sends no
// question; a code the CLI doesn't know prints as given.
func TestShowSaysWhatEachHoldCodeMeans(t *testing.T) {
	for code, want := range map[string]string{
		"client_reported_commit":       "The fix is built on a commit Volcano can't tie to a push.",
		"path":                         "It changes a path that always needs a person. (db/migrations/1.sql)",
		"sensitive_path":               "It changes a path Volcano always holds for a person, such as authentication, secret, CI or dependency files. (db/migrations/1.sql)",
		"label":                        "The pull request has the needs-human-review label.",
		"no_checks":                    "No CI ran on it.",
		"required_check_skipped":       "A required check was skipped.",
		"no_required_checks":           "The base branch requires no check, so passing checks alone don't vouch for the fix.",
		"changes_requested":            "A reviewer requested changes.",
		"repository_requires_approval": "The repository requires an approving review.",
		"person_commits":               "Someone other than LavaFlow pushed to the fix's branch.",
		"rules_bypass":                 "The Volcano GitHub App can bypass a ruleset on the base branch.",
		"unchecked_rules":              "The base branch has a rule LavaFlow doesn't check, such as required signatures.",
		"commit_convention":            "No title can meet the repository's commit convention.",
		"ungated_workflows":            "A workflow could run the fix with the repository's secrets before a person approves it, or LavaFlow couldn't check its workflows.",
		"ci_outside_actions":           "The repository could run CI, a build or a preview outside GitHub Actions, so LavaFlow never starts this fix's CI. (.travis.yml)",
		"lineage_undoes_parent":        "The fix changes lines an earlier LavaFlow fix added, which could undo it.",
		"lineage_cap":                  "This fix follows up earlier LavaFlow fixes, which together opened as many pull requests as LavaFlow allows.",
		"lineage_held":                 "An earlier LavaFlow pull request this fix follows up was held, stopped or closed.",
		"recent_regressions":           "Fixes LavaFlow merged in the project recently caused new errors.",
		"data_loss":                    "data loss",
		"new\x1b[2J":                   `new\x1b[2J`,
	} {
		t.Run(code, func(t *testing.T) {
			var paths []string
			switch code {
			case "path", "sensitive_path":
				paths = []string{"db/migrations/1.sql"}
			case "ci_outside_actions":
				paths = []string{".travis.yml"}
			}
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.holdFix(holdReason(code, "", paths...)) })

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "  Merge: held\n    - "+want+"\n")
			assert.NotContains(t, out, "\x1b")
		})
	}
}

// Each merge status reads as words, with its reason in plain words; a
// stopped fix is the person's from then on, with what they do next. One the
// CLI doesn't know prints as given, and a run without a merge prints none.
func TestShowSaysWhereTheMergeIs(t *testing.T) {
	yours := "The pull request is now yours to review and merge on GitHub."
	for _, tc := range []struct {
		status, reason, want string
	}{
		{"waiting_for_checks", "", "Merge: waiting for CI\n"},
		// A paused merge keeps its status, whose checks may have finished.
		{"waiting_for_checks", "paused", "Merge: paused while merging is off; it resumes once merging is back\n"},
		{"queued", "paused", "Merge: paused while merging is off; it resumes once merging is back\n"},
		{"queued", "", "Merge: queued to merge\n"},
		{"merged", "", "Merge: merged\n"},
		{"merged", "manual_deploy", "Merge: merged; waiting for you to deploy it\n"},
		{"stopped", "checks_failed", "Merge: stopped: a check failed\n" +
			"  The pull request is now yours: fix the failing check, then merge it or close it on GitHub.\n"},
		{"stopped", "new_reason", "Merge: stopped (new_reason)\n  " + yours + "\n"},
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
		assert.Contains(t, out, "  Opened: 2026-10-30 09:03Z\n  "+tc.want, tc.status+"/"+tc.reason)
		if tc.status != "stopped" {
			assert.NotContains(t, out, "now yours", tc.status)
		}
	}

	_, deps := newLavaflowServer(t)
	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.NotContains(t, out, "Merge:")
}

// An open fix that waits for CI while its review leaves a decision to a
// person says LavaFlow will hold it, and when, not that it waits for CI. A
// closed one, or one with nothing to decide, keeps its status.
func TestShowSaysAFixWithADecisionWillBeHeld(t *testing.T) {
	decision := []any{map[string]any{"kind": "billing", "question": "Is skipping the charge intended?"}}
	for _, tc := range []struct {
		name, reason, state string
		reasons             []any
		want                string
	}{
		{
			"waiting", "", "open", decision,
			"Merge: held for you; LavaFlow records the hold about a minute after the pull request opens\n",
		},
		{"paused", "paused", "open", decision, "Merge: held for you; LavaFlow records the hold once merging resumes\n"},
		{"nothing to decide", "", "open", nil, "Merge: waiting for CI\n"},
		{"closed", "", "closed", decision, "Merge: waiting for CI\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.runs[0]["merge"] = map[string]any{"status": "waiting_for_checks", "reason": nil}
				if tc.reason != "" {
					s.runs[0]["merge"].(map[string]any)["reason"] = tc.reason
				}
				s.runs[0]["pull_request"].(map[string]any)["state"] = tc.state
				if tc.reasons != nil {
					s.runs[0]["review"].(map[string]any)["person_reasons"] = tc.reasons
				}
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "  Opened: 2026-10-30 09:03Z\n  "+tc.want)
			assert.NotContains(t, out, "now yours")
		})
	}
}

// Every reason the API documents for a stopped merge reads as plain words,
// with the next step where it isn't reviewing and merging the pull request.
func TestShowSaysWhyTheMergeStopped(t *testing.T) {
	const thenMerge = ", then merge it or close it on GitHub."
	yours := "  The pull request is now yours to review and merge on GitHub.\n"
	next := func(step string) string { return "  The pull request is now yours: " + step + "\n" }
	follow := next("follow LavaFlow's comment on the pull request to merge it without skipping checks, or close it.")
	for reason, want := range map[string]string{
		"review_unresolved":  "LavaFlow's review didn't pass the fix\n" + next("decide whether to finish it"+thenMerge),
		"checks_failed":      "a check failed\n" + next("fix the failing check"+thenMerge),
		"checks_timed_out":   "the checks didn't finish in time\n" + next("rerun the checks"+thenMerge),
		"blocked_by_rules":   "the repository's rules block the merge\n" + yours,
		"not_mergeable":      "GitHub can't merge the pull request, for example because it conflicts with its base branch\n" + next("resolve the conflict"+thenMerge),
		"branch_behind":      "the base branch requires the fix to be up to date with it, and LavaFlow doesn't update fix branches\n" + next("update the branch"+thenMerge),
		"removed_from_queue": "the fix left the merge queue, and LavaFlow can't tell why, for example because its checks failed there or someone removed it\n" + yours,
		"reads_failed":       "LavaFlow kept failing to read the pull request, or the project's settings or repository\n" + next("check whether it is still in the merge queue"+thenMerge),
		"handed_off":         "someone else took over the pull request, by drafting it, closing it or moving it to another branch\n" + yours,
		"unreadable":         "the pull request has more checks, reviews, labels or commits than LavaFlow reads, so LavaFlow can't judge the fix\n" + yours,
		"permission_missing": "the Volcano GitHub App, or the project owner's GitHub account, lacks a permission the merge needs\n" +
			next("review it, then merge it or close it on GitHub. So later fixes can merge, grant the App's requested permissions and make sure the owner can push to the repository."),
		"repository_changed": "the project now deploys from another repository\n" + yours,
		"settings_changed": "LavaFlow was turned off or set to stop merging, the owner's plan no longer includes LavaFlow, " +
			"or the project is being deleted\n" + yours,
		"request_rejected":         "GitHub refused LavaFlow's request to start the fix's checks\n" + yours,
		"ci_start_commit_mismatch": "GitHub reports a different commit from the one LavaFlow made to start the fix's checks\n" + yours,
		"branch_refused": "LavaFlow didn't start the fix's checks because the pull request was no longer open, " +
			"or its branch wasn't one LavaFlow opened a pull request from\n" + yours,
		"head_unverified":       "GitHub doesn't report the fix's head as the signed commit LavaFlow pushed, so LavaFlow didn't start its checks\n" + yours,
		"checks_not_started":    "LavaFlow couldn't give the fix a commit message that runs CI\n" + yours,
		"queue_carries_ci_skip": "the repository's merge queue could carry an instruction to skip checks into the base branch\n" + follow,
		"merge_carries_ci_skip": "merging would carry an instruction to skip checks into the base branch, for example because the repository allows no squash merge\n" + follow,
		"paused_too_long":       "merging stayed paused for more than 7 days\n" + yours,
		"cancelled":             "the project is being deleted, so LavaFlow stopped waiting on the merge\n" + yours,
	} {
		t.Run(reason, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.holdFix()
				s.runs[0]["merge"].(map[string]any)["status"] = "stopped"
				s.runs[0]["merge"].(map[string]any)["reason"] = reason
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "  Merge: stopped: "+want)
			assert.NotContains(t, out, reason, "the code doesn't print")
			assertLavaFlowWording(t, out)

			out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
			require.NoError(t, err)
			assert.Equal(t, reason, decodeJSON(t, out)["merge"].(map[string]any)["reason"], "--json keeps the code as sent")
		})
	}
}

// A run whose pull request opened past ungated workflows lists the ones the
// API names: the file, the branch or commit whose workflows run it, and the
// job and the event when the file could be read. The API names only the first
// on each branch or commit, so show says there may be others and gives no
// count that reads as a total. A run with none says nothing about it.
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
	assert.Contains(t, out, "Ungated workflows: these jobs, and maybe others, may run this pull request with the repository's "+
		"secrets before a person reviews it. LavaFlow opened it anyway, as the project's owner allows.\n"+
		"  - .github/workflows/preview.yml on main: job deploy-preview, runs on pull_request_target\n"+
		"  - .github/workflows/broken.yml on 4f1c2b3: couldn't be read\n"+
		"  Gate them on a label a person adds.\n"+
		"Fix:\n")

	fake.set(func(s *lavaflowServer) { s.runs[0]["ungated_workflows"] = []any{} })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.NotContains(t, out, "Ungated workflows")
}

// A held or stopped fix whose pull request a person merged or closed keeps
// its merge line and reasons, says a person ended it, and doesn't say the
// pull request is still theirs to merge.
func TestShowDropsTheHandOffOnceThePullRequestCloses(t *testing.T) {
	yours := "The pull request is now yours"
	for _, status := range []string{"held", "stopped"} {
		for state, then := range map[string]string{"merged": "someone else merged it", "closed": "the pull request is closed now"} {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.holdFix(holdReason("label", "The pull request has the needs-human-review label."))
				s.runs[0]["merge"].(map[string]any)["status"] = status
				s.runs[0]["pull_request"].(map[string]any)["state"] = state
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err, status+"/"+state)
			assert.Contains(t, out, "  Merge: "+status+"; "+then+"\n"+
				"    - The pull request has the needs-human-review label.\n", status+"/"+state)
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
// do next, for every code the API documents: the exact command where one
// helps, the diagnosis or the diff where show prints them. An unknown code
// prints as given.
func TestShowSaysWhyARunOpenedNoPullRequest(t *testing.T) {
	const (
		none     = "  No fix pull request was opened"
		startRun = "volcano cloud lavaflow runs start " + lavaflowIssueID
	)
	for code, want := range map[string]string{
		"not_fixable": none + ": the diagnosis found no change in the code that fixes the error, " +
			"for example because the cause is outside it. Read the diagnosis below. If the repository already has the fix, redeploy it.\n",
		"low_confidence": none + ": the diagnosis isn't confident enough to propose a fix. Read the diagnosis below.\n",
		"no_patch":       none + ": LavaFlow found no change it could make. Make the change the diagnosis below describes yourself.\n",
		"possible_credential": none + ": the fix or its description held what looks like a credential or a project secret, " +
			"so LavaFlow pushed nothing. Write the fix yourself from the diagnosis below.\n",
		"possible_personal_data": none + ": the fix adds an email address the repository lacks, and the repository isn't private, " +
			"so LavaFlow pushed nothing. Apply the fix yourself.\n",
		"ungated_workflows": none + ": a workflow could run the fix with the repository's secrets before a person approves it.\n" +
			"  Gate those workflows on a label a person adds, or have the project's owner allow ungated workflows with " +
			"volcano cloud lavaflow settings set --allow-ungated-workflows true; then start a new run with " + startRun +
			". Or apply the fix yourself from the diff below.\n",
		"convention_unsatisfiable": none + ": LavaFlow would merge the fix, but no pull request title can meet the repository's " +
			"commit convention, so it kept the fix on this run. Apply the fix yourself from the diff below, or relax the convention.\n",
		"pull_request_open": none + ": before opening one, this run found another LavaFlow pull request for this issue that was open, " +
			"or merged and not yet deployed.\n" +
			"  See the open ones with volcano cloud lavaflow list --status pr_open. If that pull request merged, wait for its fix to deploy; " +
			"if it closed without merging, start a new run with " + startRun + ".\n",
		"source_unavailable": none + ": LavaFlow couldn't read, or open a fix on, the code that served the error.\n" +
			"  See " + noCodeGuide + " for why LavaFlow reads no code and what to do.\n",
		"permission_missing": none + ": the Volcano GitHub App, or the project owner's GitHub account, lacks a permission LavaFlow needs. " +
			"Grant the App's requested permissions, and make sure the owner can push to the repository.\n",
		"repository_changed": none + ": the project no longer deploys from the repository the run read. A new run uses the current one.\n",
		"publish_failed":     none + ": opening the pull request failed. A new run tries again.\n",
		"lineage_cap": none + ": this run follows up earlier LavaFlow fixes, which together opened as many pull requests as " +
			"LavaFlow allows. Review those pull requests, and write the rest of the fix yourself from the diagnosis below.\n",
		"lineage_held": none + ": an earlier LavaFlow pull request this fix follows up was held, stopped or closed, so LavaFlow " +
			"opens no more follow-ups to it. Apply the fix yourself from the diff below.\n",
		"review_unresolved": none + ": LavaFlow's review didn't pass this Code Quality fix. Read the diagnosis below.\n",
		"duplicate_in_repository": none + ": another project on this repository has the open Code Quality pull request for " +
			"the same finding. Review that pull request.\n",
		"security_finding": none + ": the finding has security impact and the repository isn't private, so a pull request " +
			"would disclose it. Fix it yourself from the diagnosis below.\n",
		"diagnose_only": "  Diagnosis only: the project's autonomy was set to diagnose only, so LavaFlow opened no fix. Read the diagnosis below.\n" +
			"  To have later runs open fixes, run volcano cloud lavaflow settings set --autonomy open_pr.\n",
		"new_code\x1b[2J": none + ", for a reason this version of the CLI doesn't know: new_code\\x1b[2J.\n",
	} {
		t.Run(code, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.openNoPullRequest(code) })

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Fix:\n"+want+"\nDiagnosis\n")
			assert.NotContains(t, out, "Pull request:")
			assert.NotContains(t, out, "\x1b")
			assertLavaFlowWording(t, out)
		})
	}
}

// noCodeGuide is the public guide's section on why LavaFlow read no code.
const noCodeGuide = "https://docs.volcano.dev/platform/guides/lavaflow#why-lavaflow-read-no-code"

// A run that read no code says why for each reason the API documents, names
// the deployment that served the error when the API does, and gives the next
// step with the exact command where one exists. Without a reason, or with one
// the CLI doesn't know, it names no cause and points to the guide.
func TestShowSaysWhyLavaFlowReadNoCode(t *testing.T) {
	const (
		none       = "  No fix pull request was opened: "
		deployment = "7c2e5a1f-3b8d-4e6a-9f0c-1d2e3f4a5b6c"
		newRun     = " LavaFlow starts a new run when the error happens again after that deploy.\n"
		startRun   = "volcano cloud lavaflow runs start " + lavaflowIssueID
		guide      = "  See " + noCodeGuide + " for why LavaFlow reads no code and what to do.\n"
	)
	for name, tc := range map[string]struct {
		source any
		want   string
	}{
		"no_repository": {
			source: map[string]any{"reason": "no_repository", "deployment_id": nil},
			want: none + "LavaFlow is on for no repository, so it had no code to read.\n" +
				"  Link one with volcano git connect <git-url>.\n",
		},
		"no_commit": {
			source: map[string]any{"reason": "no_commit", "deployment_id": deployment},
			want: none + "the deployment that served the error recorded no commit, so LavaFlow had no code to read.\n" +
				"  Deployment: " + deployment + "\n" +
				"  Deploy by pushing to the production branch, which volcano git status names, " +
				"or run volcano cloud deploy from a clean checkout of a commit on that branch." + newRun,
		},
		"no_commit, none serving": {
			source: map[string]any{"reason": "no_commit", "deployment_id": nil},
			want: none + "no deployment was serving when the error happened, so LavaFlow had no code to read.\n" +
				"  Deploy by pushing to the production branch, which volcano git status names, " +
				"or run volcano cloud deploy from a clean checkout of a commit on that branch." + newRun,
		},
		"uncommitted_changes": {
			source: map[string]any{"reason": "uncommitted_changes", "deployment_id": deployment},
			want: none + "the files of the deployment that served the error differ from its commit, " +
				"as when the checkout had uncommitted changes.\n" +
				"  Deployment: " + deployment + "\n" +
				"  Commit and push those changes, then run volcano cloud deploy from the clean checkout." + newRun,
		},
		"unverified_commit": {
			source: map[string]any{"reason": "unverified_commit", "deployment_id": deployment},
			want: none + "Volcano couldn't verify the commit of the deployment that served the error against the linked repository, " +
				"for example because the production branch doesn't hold it, or the deployment is a function redeploy.\n" +
				"  Deployment: " + deployment + "\n" +
				"  Push the commit to the production branch, which volcano git status names, " +
				"then run volcano cloud deploy from a clean checkout of it." + newRun,
		},
		"other_repository": {
			source: map[string]any{"reason": "other_repository", "deployment_id": deployment},
			want: none + "the commit LavaFlow would read is from a repository other than the one LavaFlow is on for, " +
				"Volcano checked it under an earlier repository link, or the project no longer links that repository.\n" +
				"  Deployment: " + deployment + "\n" +
				"  For an error a deployment served, push to the linked repository's production branch, " +
				"or run volcano cloud deploy from a clean checkout of it. " +
				"LavaFlow starts a new run when the error happens again after that deploy. " +
				"For a reported problem or a Code Quality finding, start a new run with " + startRun +
				" while the project links the repository LavaFlow is on for.\n",
		},
		"repository_unreadable": {
			source: map[string]any{"reason": "repository_unreadable", "deployment_id": nil},
			want: none + "LavaFlow couldn't read the repository at the commit, " +
				"for example because the Volcano GitHub App lost access to it.\n" +
				"  Check the project's repository link with volcano git status and the Volcano GitHub App's access " +
				"to the repository, then start a new run with " + startRun + ".\n",
		},
		"commit_not_on_production_branch": {
			source: map[string]any{"reason": "commit_not_on_production_branch", "deployment_id": nil},
			want: none + "the production branch no longer holds the commit the fix is on.\n" +
				"  Deploy from the production branch: push to it, or run volcano cloud deploy from a clean checkout of it." + newRun,
		},
		"deployment_unattributed": {
			source: map[string]any{"reason": "deployment_unattributed", "deployment_id": nil},
			want: none + "a newer deployment was going live when the error happened, " +
				"so LavaFlow couldn't tell which deployment served it.\n" +
				"  If the error happens again, LavaFlow may start one more run on its own; " +
				"if this run already followed another like it, the next run waits for a new deploy.\n",
		},
		"no reason": {
			source: nil,
			want:   none + "LavaFlow couldn't read, or open a fix on, the code that served the error.\n" + guide,
		},
		"unknown reason": {
			source: map[string]any{"reason": "new_reason\x1b[2J\u202e", "deployment_id": deployment},
			want: none + "LavaFlow couldn't read, or open a fix on, the code that served the error. " +
				`Its reason, new_reason\x1b[2J\u202e, is one this version of the CLI doesn't know.` + "\n" +
				"  Deployment: " + deployment + "\n" + guide,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.openNoPullRequest("source_unavailable")
				s.runs[0]["source_unavailable"] = tc.source
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Fix:\n"+tc.want+"\nDiagnosis\n")
			assert.NotContains(t, out, "\x1b")
			assert.NotContains(t, out, "\u202e")
			if name == "no reason" || name == "unknown reason" {
				assert.NotContains(t, out, "repository connection")
			}
			assertLavaFlowWording(t, out)
		})
	}
}

// A run held for its workflows lists every job the API names under the Fix
// line, then the ways forward. When LavaFlow found none on a second read, it
// says so, and when it couldn't read them all, it names none.
func TestShowListsTheJobsThatHeldTheFix(t *testing.T) {
	const gate = "  Gate those workflows on a label a person adds, or have the project's owner allow ungated workflows with " +
		"volcano cloud lavaflow settings set --allow-ungated-workflows true; then start a new run with " +
		"volcano cloud lavaflow runs start " + lavaflowIssueID + ". Or apply the fix yourself from the diff below.\n"
	jobs := []any{
		map[string]any{"source": "main", "path": ".github/workflows/ci.yml", "job": "test", "trigger": "pull_request_target"},
		map[string]any{"source": "main", "path": ".github/workflows/ci.yml", "job": "lint", "trigger": "pull_request_target"},
		map[string]any{"source": "4f1c2b3", "path": ".github/workflows/\x1b[2Jbroken.yml"},
	}
	for name, tc := range map[string]struct {
		held any
		want string
	}{
		"jobs": {
			held: map[string]any{"jobs": jobs, "jobs_sha256": "abc", "read_at": []any{}},
			want: "  No fix pull request was opened: these workflow jobs could run the fix with the repository's secrets before a person approves it:\n" +
				"    - .github/workflows/ci.yml on main: job test, runs on pull_request_target\n" +
				"    - .github/workflows/ci.yml on main: job lint, runs on pull_request_target\n" +
				`    - .github/workflows/\x1b[2Jbroken.yml on 4f1c2b3: couldn't be read` + "\n" + gate,
		},
		"none found again": {
			held: map[string]any{"jobs": []any{}, "jobs_sha256": "abc", "read_at": []any{}},
			want: "  No fix pull request was opened: a workflow could run the fix with the repository's secrets before a person approves it. " +
				"When LavaFlow read the workflows again, it found no such job, so they may have changed since.\n" +
				"  Start a new run with volcano cloud lavaflow runs start " + lavaflowIssueID + ".\n",
		},
		"not read to the end": {
			held: nil,
			want: "  No fix pull request was opened: a workflow could run the fix with the repository's secrets before a person approves it.\n" + gate,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.openNoPullRequest("ungated_workflows")
				s.runs[0]["held_workflows"] = tc.held
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Fix:\n"+tc.want+"\nDiagnosis\n")
			assert.NotContains(t, out, "\x1b")

			out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
			require.NoError(t, err)
			got := decodeJSON(t, out)
			assert.Contains(t, got, "held_workflows")
			if tc.held != nil {
				assert.Equal(t, asJSON(t, tc.held.(map[string]any)), got["held_workflows"], "--json keeps the jobs as sent")
			}
		})
	}
}

// A reason that points to the diagnosis or the diff leaves the pointer out
// when show prints neither.
func TestShowPointsToADiagnosisOnlyWhenItPrintsOne(t *testing.T) {
	for code, want := range map[string]string{
		"possible_credential": "so LavaFlow pushed nothing. Write the fix yourself.\n",
		"no_patch":            "No fix pull request was opened: LavaFlow found no change it could make.\n",
		"not_fixable":         "for example because the cause is outside it. If the repository already has the fix, redeploy it.\n",
		"lineage_held":        "so LavaFlow opens no more follow-ups to it. Apply the fix yourself.\n",
		"diagnose_only":       "so LavaFlow opened no fix.\n  To have later runs open fixes, run volcano cloud lavaflow settings set --autonomy open_pr.\n",
	} {
		t.Run(code, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.openNoPullRequest(code)
				s.runs[0]["diagnosis"] = nil
				s.runs[0]["diff"] = nil
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, want)
			assert.NotContains(t, out, "below")
		})
	}
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
// cancelled says so, with the API's words for why on the same line, and a
// cancelled run says how to try again.
func TestShowSaysARunWithoutAPullRequestHasNoFix(t *testing.T) {
	again := "  To try again, start a new run with volcano cloud lavaflow runs start " + lavaflowIssueID + ".\n"
	for status, want := range map[string]string{
		"queued":     "  No fix yet: LavaFlow is still working on this run.\n",
		"diagnosing": "  No fix yet: LavaFlow is still working on this run.\n",
		"fixing":     "  No fix yet: LavaFlow is still working on this run.\n",
		"reviewing":  "  No fix yet: LavaFlow is still working on this run.\n",
		"cancelled":  "  No fix pull request was opened: the run was cancelled.\n" + again,
		"new_stage":  "  No fix pull request was opened.\n",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.openNoPullRequest(nil)
			s.runs[0]["status"] = status
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, status)
		assert.Contains(t, out, "Fix:\n"+want+"\nDiagnosis\n", status)
	}

	_, deps := newLavaflowServer(t)
	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowFailedID)
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(out, "Fix:\n  No fix pull request was opened: the run failed. "+lavaflowFailureMessage+"\n"), out)
}

// A cancelled run says why on its Fix line, in the API's words, not as a
// warning that reads as current, and says how to try again unless its issue
// is gone. A failure on a run with a pull request still warns.
func TestShowFoldsWhyARunWasCancelledIntoTheFix(t *testing.T) {
	for code, want := range map[string]string{
		"lavaflow_disabled": "Fix:\n  No fix pull request was opened: the run was cancelled. LavaFlow was turned off for this project.\n" +
			"  To try again, start a new run with volcano cloud lavaflow runs start " + lavaflowIssueID + ".\n",
		"issue_not_found": "Fix:\n  No fix pull request was opened: the run was cancelled. The issue no longer exists.\n\nDiagnosis\n",
	} {
		t.Run(code, func(t *testing.T) {
			message := map[string]string{
				"lavaflow_disabled": "LavaFlow was turned off for this project.",
				"issue_not_found":   "The issue no longer exists.",
			}[code]
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.openNoPullRequest(nil)
				s.runs[0]["status"] = "cancelled"
				s.runs[0]["failure"] = map[string]any{"code": code, "message": message}
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Status: Cancelled\n")
			assert.Contains(t, out, want)
			assert.NotContains(t, out, "Warning:")
		})
	}

	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["failure"] = map[string]any{"code": "feature_paused", "message": "LavaFlow is paused. This run continues when LavaFlow resumes."}
	})
	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "\nWarning: LavaFlow is paused. This run continues when LavaFlow resumes.\n")
}

// show labels a run's status as list does: a held or stopped fix whose pull
// request is open reads as the person's in both.
func TestShowLabelsTheRunAsListDoes(t *testing.T) {
	for status, want := range map[string]string{"held": "Held: yours to merge", "stopped": "Stopped: yours to merge"} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.holdFix(holdReason("label", "The pull request has the needs-human-review label."))
			s.runs[0]["merge"].(map[string]any)["status"] = status
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, status)
		assert.Contains(t, out, "\nStatus: "+want+"\n", status)

		out, err = executeLavaflowCommand(t, New(deps), "list")
		require.NoError(t, err, status)
		assert.Contains(t, out, lavaflowRunID+"  "+want+" ", status)
	}
}

// A run whose resource name wasn't recorded says so, in show and list,
// rather than print a blank.
func TestARunSaysWhenItsResourceNameWasntRecorded(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["resource"] = map[string]any{"id": lavaflowResourceID, "type": "function", "name": ""}
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "\nResource: function (name not recorded)\n")

	out, err = executeLavaflowCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Regexp(t, lavaflowRunID+`\s+PR open\s+#42 open\s+\(name not recorded\)\s+2026-10-30 09:00Z`, out)
}

// show says when the run was queued and finished, in UTC marked Z.
func TestShowSaysWhenTheRunWasQueuedAndFinished(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "\nQueued: 2026-10-30 09:00Z\nFinished: 2026-10-30 09:03Z\n")
	assert.NotContains(t, out, "Started:")
}

// A review of one round says so in the singular.
func TestShowSaysOneRound(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs[0]["review"] = map[string]any{"verdict": "passed", "rounds": 1} })

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "\nReview: passed after 1 round\n")
}

// A draft LavaFlow left for a person to mark ready reads as one its review
// didn't pass when the review above says so, rather than ask to mark it
// ready.
func TestShowDoesntAskToMarkReadyAFixItsReviewDidntPass(t *testing.T) {
	for name, review := range map[string]map[string]any{
		"blocked":           {"verdict": "blocked", "rounds": 3, "stopped": "rounds"},
		"changes requested": {"verdict": "changes_requested", "rounds": 3, "stopped": "rounds"},
		"no reviewer":       {"verdict": "error", "rounds": 0, "stopped": "too_few_reviewers"},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.runs[0]["review"] = review
			pr := s.runs[0]["pull_request"].(map[string]any)
			pr["draft"], pr["draft_reason"] = true, "open_pr"
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err, name)
		assert.Contains(t, out, "  State: draft (LavaFlow's review didn't pass the fix, so LavaFlow won't merge it; decide whether to finish it)\n", name)
		assert.NotContains(t, out, "mark it ready", name)
	}
}

// A merged fix says when it merged and, once the API has them, when it went
// live and where its issue stands.
func TestShowSaysWhenTheMergedFixWentLive(t *testing.T) {
	merged := "  Merge: merged by LavaFlow\n  Merged: 2026-10-30 09:15Z\n"
	for name, tc := range map[string]struct {
		deployedAt any
		issue      any
		want       string
	}{
		"not live yet": {nil, nil, merged + "\nDiagnosis\n"},
		"live":         {"2026-10-30T09:20:00Z", nil, merged + "  Deployed: 2026-10-30 09:20Z\n"},
		"resolved": {
			"2026-10-30T09:20:00Z", lavaflowIssue("resolved", nil),
			merged + "  Deployed: 2026-10-30 09:20Z; the issue is resolved\n",
		},
		"came back": {
			"2026-10-30T09:20:00Z", lavaflowIssue("open", "2026-10-31T10:00:00Z"),
			merged + "  Deployed: 2026-10-30 09:20Z; the error came back at 2026-10-31 10:00Z, so the issue is open again\n",
		},
		"open, came back before": {
			"2026-10-30T09:20:00Z", lavaflowIssue("open", "2026-10-29T10:00:00Z"),
			merged + "  Deployed: 2026-10-30 09:20Z\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.runs[0]["status"] = "completed"
				pr := s.runs[0]["pull_request"].(map[string]any)
				pr["state"], pr["merged_at"], pr["deployed_at"] = "merged", "2026-10-30T09:15:00Z", tc.deployedAt
				s.runs[0]["merge"] = map[string]any{"status": "merged", "reason": nil, "hold_reasons": []any{}, "merged_by_lavaflow": true}
				s.runs[0]["issue"] = tc.issue
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "  State: merged\n  Opened: 2026-10-30 09:03Z\n"+tc.want)
		})
	}
}

// lavaflowIssue is a run's issue as the API summarizes it.
func lavaflowIssue(status string, regressedAt any) map[string]any {
	return map[string]any{
		"id": lavaflowIssueID, "title": lavaflowIssueTitle, "status": status, "culprit": nil, "event_count": 3,
		"last_seen_at": "2026-10-30T08:59:00Z", "regressed_at": regressedAt, "resolved_at": nil, "run_count": 1,
	}
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

// show says when the repository's own checks didn't run on the fix, in the
// API's words, and says so itself when the API sends none. A run with no fix
// has none, and prints no line. The API asks clients to read a status or
// reason they don't know as not_run.
func TestShowSaysTheRepositorysTestsDidntRun(t *testing.T) {
	for name, checks := range map[string]any{
		"not run":        checksNotRun(),
		"unknown status": map[string]any{"status": "queued", "reason": "sandbox_unavailable", "message": lavaflowChecksMessage},
		"unknown reason": map[string]any{"status": "not_run", "reason": "runner_busy", "message": lavaflowChecksMessage},
		"newer fields":   map[string]any{"status": "not_run", "reason": "sandbox_unavailable", "message": lavaflowChecksMessage, "ran_at": nil},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = checks })

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Review: passed after 2 rounds\nChecks: "+lavaflowChecksMessage+"\nFix:\n")
			assert.NotContains(t, out, "not_run")
			assert.NotContains(t, out, "sandbox_unavailable")
		})
	}

	t.Run("no pull request", func(t *testing.T) {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.openNoPullRequest("ungated_workflows")
			s.runs[0]["repository_checks"] = map[string]any{
				"status": "not_run", "reason": "sandbox_unavailable", "message": "LavaFlow didn't run the repository's tests.",
			}
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err)
		assert.Contains(t, out, "Checks: LavaFlow didn't run the repository's tests.\nFix:\n  No fix pull request was opened")
	})

	t.Run("no message", func(t *testing.T) {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.runs[0]["repository_checks"] = map[string]any{"status": "not_run", "reason": "sandbox_unavailable", "message": " "}
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err)
		assert.Contains(t, out, "Checks: The repository's tests didn't run.\nFix:\n")
	})

	t.Run("control characters", func(t *testing.T) {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.runs[0]["repository_checks"] = map[string]any{"status": "not_run", "reason": "sandbox_unavailable", "message": "push\x1b[2J\rnow"}
		})

		out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
		require.NoError(t, err)
		assert.Contains(t, out, `Checks: push\x1b[2J\x0dnow`)
		assert.NotContains(t, out, "\x1b")
	})

	for name, checks := range map[string]any{"null": nil, "absent": "absent"} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				if checks == "absent" {
					delete(s.runs[0], "repository_checks")
					return
				}
				s.runs[0]["repository_checks"] = checks
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.NotContains(t, out, "Checks:")
			assert.Contains(t, out, "Review: passed after 2 rounds\nFix:\n")
		})
	}
}

// --json keeps repository_checks as the API sent it, fields newer than the
// CLI's contract included.
func TestShowJSONKeepsTheRepositoryChecks(t *testing.T) {
	checks := map[string]any{"status": "queued", "reason": "runner_busy", "message": lavaflowChecksMessage, "ran_at": "2026-11-06T10:00:00Z"}
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = checks })

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
	require.NoError(t, err)
	assert.Equal(t, asJSON(t, checks), decodeJSON(t, out)["repository_checks"])

	fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = nil })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
	require.NoError(t, err)
	got := decodeJSON(t, out)
	assert.Contains(t, got, "repository_checks")
	assert.Nil(t, got["repository_checks"])
}

// The API's words for the repository's checks on a fix that opened a pull
// request, and the sentences Volcano serves in place of check text it
// withholds.
const (
	checksPassedMessage  = "LavaFlow ran the repository's checks before opening this pull request, and they passed."
	checksFailedMessage  = "LavaFlow ran the repository's checks before opening this pull request, and they failed."
	checksNoneMessage    = "LavaFlow found no checks to run in the repository before opening this pull request."
	checksRevisedMessage = "The repository's checks failed on an earlier version of this fix. LavaFlow revised the fix " +
		"once and opened this pull request without running the checks again."
	checkCommandWithheld = "LavaFlow left this command out because it may contain a credential"
	checkOutputWithheld  = "LavaFlow left this output out because it may contain a credential"
)

// failedChecks is repository_checks for checks that failed, as the API serves
// it: a command that passed, one that failed and printed, and one that timed
// out with no exit code and printed nothing.
func failedChecks() map[string]any {
	return map[string]any{
		"status": "failed", "message": checksFailedMessage,
		"commands": []any{
			map[string]any{"command": "npm run lint", "status": "passed", "exit_code": 0},
			map[string]any{
				"command": "npm test", "status": "failed", "exit_code": 1,
				"output": "FAIL src/cart.test.ts\n  charges nothing for an empty cart\n    Expected: 0\n    Received: 12.5\n",
			},
			map[string]any{"command": "npm run e2e", "status": "timed_out", "exit_code": nil, "output": ""},
		},
	}
}

// failedChecksLines is how show lists failedChecks' commands.
const failedChecksLines = "  - passed, exit code 0: npm run lint\n" +
	"  - failed, exit code 1: npm test\n" +
	"    Output:\n" +
	"      FAIL src/cart.test.ts\n" +
	"        charges nothing for an empty cart\n" +
	"          Expected: 0\n" +
	"          Received: 12.5\n" +
	"  - timed out: npm run e2e\n" +
	"    Output: (empty)\n"

// show lists, under the Checks line, each command the repository's checks
// ran, in the order the API sends them, with how it ended and its exit code,
// and the output of one that failed or timed out indented under it. --json
// keeps the checks as sent.
func TestShowListsTheCommandsTheRepositorysChecksRan(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = failedChecks() })

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Review: passed after 2 rounds\nChecks: "+checksFailedMessage+"\n"+failedChecksLines+"Fix:\n")
	assert.NotContains(t, out, "timed_out")
	assertLavaFlowWording(t, out)

	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
	require.NoError(t, err)
	assert.Equal(t, asJSON(t, failedChecks()), decodeJSON(t, out)["repository_checks"], "--json keeps the checks as sent")
}

// Each status prints the API's message and the commands it sent. A revised
// fix's checks print as served, with nothing added: the message already says
// the commands ran on an earlier version of the fix. A status the CLI doesn't
// know prints its message too.
func TestShowSaysWhatTheRepositorysChecksFound(t *testing.T) {
	for name, tc := range map[string]struct {
		checks map[string]any
		want   string
	}{
		"passed": {
			checks: map[string]any{"status": "passed", "message": checksPassedMessage, "commands": []any{
				map[string]any{"command": "npm run lint", "status": "passed", "exit_code": 0},
				map[string]any{"command": "npm test", "status": "passed", "exit_code": 0},
			}},
			want: "Checks: " + checksPassedMessage + "\n" +
				"  - passed, exit code 0: npm run lint\n" +
				"  - passed, exit code 0: npm test\n",
		},
		"failed": {checks: failedChecks(), want: "Checks: " + checksFailedMessage + "\n" + failedChecksLines},
		"no checks found": {
			checks: map[string]any{"status": "no_checks_found", "message": checksNoneMessage},
			want:   "Checks: " + checksNoneMessage + "\n",
		},
		"not run": {checks: checksNotRun(), want: "Checks: " + lavaflowChecksMessage + "\n"},
		"revised": {
			checks: map[string]any{"status": "failed", "revised": true, "message": checksRevisedMessage, "commands": []any{
				map[string]any{"command": "npm test", "status": "failed", "exit_code": 1, "output": "FAIL src/cart.test.ts\n"},
			}},
			want: "Checks: " + checksRevisedMessage + "\n" +
				"  - failed, exit code 1: npm test\n" +
				"    Output:\n" +
				"      FAIL src/cart.test.ts\n",
		},
		"unknown status": {
			checks: map[string]any{"status": "cancelled", "message": lavaflowChecksMessage},
			want:   "Checks: " + lavaflowChecksMessage + "\n",
		},
		"not run, revised, unknown reason": {
			checks: map[string]any{
				"status": "not_run", "reason": "fix_revised", "revised": true,
				"message": "LavaFlow revised the fix and didn't run the repository's checks on it again.",
			},
			want: "Checks: LavaFlow revised the fix and didn't run the repository's checks on it again.\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = tc.checks })

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Review: passed after 2 rounds\n"+tc.want+"Fix:\n")
			for _, code := range []string{"no_checks_found", "not_run", "sandbox_unavailable", "cancelled", "fix_revised"} {
				assert.NotContains(t, out, code)
			}
			assertLavaFlowWording(t, out)
		})
	}
}

// Without the API's message, the Checks line says what the status means, and
// reads a status the CLI doesn't know as not run. A revised fix's line says
// the checks failed on an earlier version, not on the fix as saved.
func TestShowSaysWhatTheRepositorysChecksFoundWithoutAMessage(t *testing.T) {
	for name, tc := range map[string]struct {
		checks map[string]any
		want   string
	}{
		"passed":          {map[string]any{"status": "passed", "message": ""}, "The repository's checks passed."},
		"failed":          {map[string]any{"status": "failed", "message": ""}, "The repository's checks failed."},
		"revised":         {map[string]any{"status": "failed", "revised": true, "message": " "}, "The repository's checks failed on an earlier version of this fix."},
		"no checks found": {map[string]any{"status": "no_checks_found", "message": ""}, "LavaFlow found no checks to run in the repository."},
		"not run":         {map[string]any{"status": "not_run", "reason": "run_deadline", "message": ""}, "The repository's tests didn't run."},
		"unknown status":  {map[string]any{"status": "cancelled", "message": ""}, "The repository's tests didn't run."},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = tc.checks })

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "\nChecks: "+tc.want+"\nFix:\n")
		})
	}
}

// A command status the CLI doesn't know reads as failed, as the API asks, and
// a command with no exit code shows none.
func TestShowReadsACheckCommandStatusItDoesntKnowAsFailed(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["repository_checks"] = map[string]any{"status": "failed", "message": checksFailedMessage, "commands": []any{
			map[string]any{"command": "npm test", "status": "skipped", "exit_code": nil},
			map[string]any{"command": "npm run e2e", "status": "killed", "exit_code": 137, "output": "Killed\n"},
		}}
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Checks: "+checksFailedMessage+"\n"+
		"  - failed: npm test\n"+
		"  - failed, exit code 137: npm run e2e\n"+
		"    Output:\n"+
		"      Killed\n"+
		"Fix:\n")
	assert.NotContains(t, out, "skipped")
	assert.NotContains(t, out, "killed")
}

// A command or output Volcano withheld prints as the sentence it serves in
// its place.
func TestShowPrintsWithheldCheckTextAsServed(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["repository_checks"] = map[string]any{"status": "failed", "message": checksFailedMessage, "commands": []any{
			map[string]any{"command": checkCommandWithheld, "status": "passed", "exit_code": 0},
			map[string]any{"command": "npm test", "status": "failed", "exit_code": 1, "output": checkOutputWithheld},
			map[string]any{"command": checkCommandWithheld, "status": "timed_out", "exit_code": nil, "output": checkOutputWithheld},
		}}
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Checks: "+checksFailedMessage+"\n"+
		"  - passed, exit code 0: "+checkCommandWithheld+"\n"+
		"  - failed, exit code 1: npm test\n"+
		"    Output:\n"+
		"      "+checkOutputWithheld+"\n"+
		"  - timed out: "+checkCommandWithheld+"\n"+
		"    Output:\n"+
		"      "+checkOutputWithheld+"\n"+
		"Fix:\n")
}

// checkTextWithControls is a failed command whose text and output carry ANSI
// escape sequences, C1 and bidirectional controls, and lines that read like
// the CLI's own.
func checkTextWithControls() map[string]any {
	return map[string]any{"status": "failed", "message": checksFailedMessage, "commands": []any{
		map[string]any{
			"command": "npm test -- \x1b[31m--color\x1b[0m \x1b[2J\x1b[H\x1b]0;title\x07 \u202eltl\u202c\nChecks: " + checksPassedMessage,
			"status":  "failed", "exit_code": 1,
			"output": "\x1b[31mFAIL\x1b[0m src/cart.test.ts\n" +
				"\x1b[1A\x1b[2Kall passed\rok\u009b2J\n" +
				"expected \u202bthe admin\u202c and \u2066the owner\u2069 \u202dLTR\u202c \u202aembed\u202c \u2067rtl\u2069 \u2068first\u2069\n" +
				"Checks: " + checksPassedMessage + "\n" +
				"Fix:\n",
		},
	}}
}

// A command and its output are the repository's text: an escape sequence, such
// as a color or a cursor movement, and a bidirectional control print as
// visible escapes, a command stays on one line, and each line of output is
// indented under the command, so none of it can pass for the CLI's own lines.
func TestShowPrintsCheckCommandsAndOutputAsPlainText(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = checkTextWithControls() })

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Checks: "+checksFailedMessage+"\n"+
		`  - failed, exit code 1: npm test -- \x1b[31m--color\x1b[0m \x1b[2J\x1b[H\x1b]0;title\x07 \u202eltl\u202c\x0aChecks: `+checksPassedMessage+"\n"+
		"    Output:\n"+
		`      \x1b[31mFAIL\x1b[0m src/cart.test.ts`+"\n"+
		`      \x1b[1A\x1b[2Kall passed\x0dok\u009b2J`+"\n"+
		`      expected \u202bthe admin\u202c and \u2066the owner\u2069 \u202dLTR\u202c \u202aembed\u202c \u2067rtl\u2069 \u2068first\u2069`+"\n"+
		"      Checks: "+checksPassedMessage+"\n"+
		"      Fix:\n"+
		"Fix:\n")
	for _, raw := range []string{
		"\x1b", "\r", "\x07", "\u009b",
		"\u202a", "\u202b", "\u202c", "\u202d", "\u202e", "\u2066", "\u2067", "\u2068", "\u2069",
	} {
		assert.NotContains(t, out, raw)
	}
	assert.NotContains(t, out, "\nChecks: "+checksPassedMessage)
}

// --json prints a command and its output as the API sent them, only
// JSON-encoded: they decode to the same strings, carry no raw C0 or C1
// control, and none of show's visible escapes.
func TestShowJSONKeepsCheckTextAsSent(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = checkTextWithControls() })

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
	require.NoError(t, err)
	got := decodeJSON(t, out)["repository_checks"].(map[string]any)["commands"].([]any)[0].(map[string]any)
	sent := checkTextWithControls()["commands"].([]any)[0].(map[string]any)
	assert.Equal(t, sent["command"], got["command"])
	assert.Equal(t, sent["output"], got["output"])
	for _, raw := range []string{"\x1b", "\r", "\x07", "\u009b"} {
		assert.NotContains(t, out, raw)
	}
	for _, shown := range []string{`\\x1b`, `\\x0d`, `\\u202e`, `\\u2066`} {
		assert.NotContains(t, out, shown)
	}
}

// Checks with no commands, an empty list included, list none, and a command
// with no output prints no Output line.
func TestShowPrintsNoCheckCommandsTheAPILeavesOut(t *testing.T) {
	for name, tc := range map[string]struct {
		checks map[string]any
		want   string
	}{
		"absent commands": {
			checks: map[string]any{"status": "failed", "message": checksFailedMessage},
			want:   "Checks: " + checksFailedMessage + "\nFix:\n",
		},
		"empty commands": {
			checks: map[string]any{"status": "failed", "message": checksFailedMessage, "commands": []any{}},
			want:   "Checks: " + checksFailedMessage + "\nFix:\n",
		},
		"absent output": {
			checks: map[string]any{"status": "failed", "message": checksFailedMessage, "commands": []any{
				map[string]any{"command": "npm test", "status": "failed", "exit_code": 1},
			}},
			want: "Checks: " + checksFailedMessage + "\n  - failed, exit code 1: npm test\nFix:\n",
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.runs[0]["repository_checks"] = tc.checks })

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Review: passed after 2 rounds\n"+tc.want)
			assert.NotContains(t, out, "Output")
		})
	}
}

// show says in plain words why a review ended without passing its fix. A
// passed review has no reason and prints no line; a reason the CLI doesn't
// know prints as other, with its code.
func TestShowSaysWhyTheReviewStopped(t *testing.T) {
	for stopped, want := range map[string]string{
		"rounds":            "The last review round still found problems.",
		"no_revision":       "LavaFlow gave no revised fix.",
		"budget_exhausted":  "The run used its token budget before LavaFlow could revise the fix, not the daily spend limit.",
		"context_limit":     "The fix's conversation grew too long for LavaFlow before it could revise it.",
		"timed_out":         "The run's time for review ran out.",
		"reviewers_failed":  "No reviewer of the last round finished.",
		"too_few_reviewers": "Fewer reviewers could run than a round needs to pass the fix.",
		"unreadable":        "A retried run couldn't read the review saved with its fix, so a person must review it.",
		"future_reason":     "Other (future reason).",
		"future\x1b[2J":     `Other (future\x1b[2J).`,
	} {
		t.Run(stopped, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.runs[0]["review"] = map[string]any{"verdict": "blocked", "rounds": 3, "stopped": stopped}
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Review: blocked after 3 rounds\nReview stopped: "+want+"\nChecks: ")
			assert.NotContains(t, out, "\x1b")
			assertLavaFlowWording(t, out)

			out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
			require.NoError(t, err)
			assert.Equal(t, stopped, decodeJSON(t, out)["review"].(map[string]any)["stopped"], "--json keeps the code as sent")
		})
	}

	for name, review := range map[string]map[string]any{
		"null":   {"verdict": "passed", "rounds": 2, "stopped": nil},
		"absent": {"verdict": "passed", "rounds": 2},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.runs[0]["review"] = review })

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Review: passed after 2 rounds\nChecks: ")
			assert.NotContains(t, out, "Review stopped")
		})
	}
}

// A review with no round says no reviewer could run, rather than counting
// zero rounds.
func TestShowSaysWhenNoReviewerCouldRun(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["review"] = map[string]any{"verdict": "error", "rounds": 0, "stopped": "too_few_reviewers"}
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Review: error; no reviewer could run\nReview stopped: ")
	assert.NotContains(t, out, "0 rounds")
}

// The sentences Volcano serves in place of review text it withholds.
const (
	findingWithheld  = "LavaFlow left this explanation out because it may contain a credential"
	questionWithheld = "LavaFlow left this question out because it may contain a credential"
)

// blockedReview is a review that left findings open and asks a person to
// decide, as the API serves it, withheld text included.
func blockedReview() map[string]any {
	return map[string]any{
		"verdict": "blocked",
		"rounds":  3,
		"stopped": "rounds",
		"open_findings": []any{
			map[string]any{
				"severity": "major", "file": "src/cart.ts", "line": 42,
				"explanation": "An empty cart still reaches chargeCustomer, which reads cart.total before the new guard runs.",
			},
			map[string]any{"severity": "minor", "file": nil, "line": nil, "explanation": findingWithheld},
		},
		"person_reasons": []any{
			map[string]any{"kind": "billing", "question": "The fix skips the charge for an empty cart. Is that intended?"},
			map[string]any{"kind": "trust_boundary", "question": questionWithheld},
		},
	}
}

// show lists the findings a review left open and what it leaves to a person,
// under the review, in the order the API sends them. Withheld text prints as
// Volcano's sentence, and --json keeps both lists as sent.
func TestShowListsTheReviewsOpenFindingsAndQuestions(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.runs[0]["review"] = blockedReview() })

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Review: blocked after 3 rounds\n"+
		"Review stopped: The last review round still found problems.\n"+
		"Open findings:\n"+
		"  - major, src/cart.ts:42: An empty cart still reaches chargeCustomer, which reads cart.total before the new guard runs.\n"+
		"  - minor: "+findingWithheld+"\n"+
		"For you to decide:\n"+
		"  - effect on payments, pricing, metering, or charges: The fix skips the charge for an empty cart. Is that intended?\n"+
		"  - effect on authentication, authorization, secrets, or access: "+questionWithheld+"\n"+
		"Checks: ")
	assertLavaFlowWording(t, out)

	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowRunID, "--json")
	require.NoError(t, err)
	assert.Equal(t, asJSON(t, blockedReview()), decodeJSON(t, out)["review"], "--json keeps the review as sent")
}

// Each finding says where it points: a file and line, a file with no one
// line, a path Volcano doesn't show, or nothing for a finding about no file.
func TestShowSaysWhereEachOpenFindingPoints(t *testing.T) {
	for name, tc := range map[string]struct {
		file, line any
		want       string
	}{
		"file and line":         {"src/cart.ts", 42, "  - major, src/cart.ts:42: Explained.\n"},
		"no line":               {"src/cart.ts", nil, "  - major, src/cart.ts: Explained.\n"},
		"no file":               {nil, nil, "  - major: Explained.\n"},
		"path not shown":        {"", nil, "  - major, path not shown: Explained.\n"},
		"path not shown, line":  {"", 7, "  - major, path not shown, line 7: Explained.\n"},
		"line without a file":   {nil, 7, "  - major, line 7: Explained.\n"},
		"control chars in path": {"src/\x1b[2Jcart.ts", 3, "  - major, src/\\x1b[2Jcart.ts:3: Explained.\n"},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.runs[0]["review"] = map[string]any{
					"verdict": "blocked", "rounds": 3, "stopped": "rounds",
					"open_findings": []any{map[string]any{"severity": "major", "file": tc.file, "line": tc.line, "explanation": "Explained."}},
				}
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Open findings:\n"+tc.want+"Checks: ")
			assert.NotContains(t, out, "\x1b")
		})
	}
}

// A finding's explanation and a question are the reviewer's words: a control
// character prints as a visible escape, and text on more than one line
// continues indented under its item.
func TestShowPrintsReviewTextAsPlainText(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["review"] = map[string]any{
			"verdict": "blocked", "rounds": 3, "stopped": "rounds",
			"open_findings": []any{map[string]any{
				"severity": "critical", "file": "a.ts", "line": 1,
				"explanation": "\x1b[31mred\x1b[0m \x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\\rover\u009b2J\nsecond line\n",
			}},
			"person_reasons": []any{map[string]any{"kind": "billing\x1b]0;title\x07", "question": "Charge\x1b[2J twice?"}},
		}
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Open findings:\n"+
		`  - critical, a.ts:1: \x1b[31mred\x1b[0m \x1b]8;;https://example.com\x1b\link\x1b]8;;\x1b\\x0dover\u009b2J`+"\n"+
		"    second line\n"+
		"For you to decide:\n"+
		`  - billing\x1b]0;title\x07: Charge\x1b[2J twice?`+"\n")
	for _, raw := range []string{"\x1b", "\r", "\x07", "\u009b"} {
		assert.NotContains(t, out, raw)
	}
}

// Each kind of decision reads as words; a kind the CLI doesn't know prints as
// the API gives it.
func TestShowNamesWhatEachDecisionIsAbout(t *testing.T) {
	for kind, want := range map[string]string{
		"irreversible":    "whether a revert can undo it",
		"trust_boundary":  "effect on authentication, authorization, secrets, or access",
		"breaks_contract": "effect on interfaces other code or users depend on",
		"billing":         "effect on payments, pricing, metering, or charges",
		"unexplained":     "behavior its context doesn't explain",
		"not_reviewable":  "what the reviewers couldn't review closely",
		"data_residency":  "data_residency",
	} {
		t.Run(kind, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.runs[0]["review"] = map[string]any{
					"verdict": "blocked", "rounds": 3, "stopped": "rounds",
					"person_reasons": []any{map[string]any{"kind": kind, "question": "Is that intended?"}},
				}
			})

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Review stopped: The last review round still found problems.\n"+
				"For you to decide:\n  - "+want+": Is that intended?\nChecks: ")
			assert.NotContains(t, out, "Open findings")
		})
	}
}

// A review that lists no findings or questions, absent or empty, prints no
// heading for them.
func TestShowPrintsNoReviewListsTheAPILeavesOut(t *testing.T) {
	for name, review := range map[string]map[string]any{
		"absent": {"verdict": "passed", "rounds": 2},
		"empty":  {"verdict": "passed", "rounds": 2, "open_findings": []any{}, "person_reasons": []any{}},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) { s.runs[0]["review"] = review })

			out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
			require.NoError(t, err)
			assert.Contains(t, out, "Review: passed after 2 rounds\nChecks: ")
			assert.NotContains(t, out, "Open findings")
			assert.NotContains(t, out, "For you to decide")
		})
	}
}

// A bidirectional embedding or isolate in review text prints as a visible
// escape, so the text shows in the order it was written.
func TestShowPrintsBidirectionalControlsAsEscapes(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.runs[0]["review"] = map[string]any{
			"verdict": "blocked", "rounds": 3, "stopped": "rounds",
			"open_findings": []any{map[string]any{
				"severity": "major", "file": "a.ts", "line": 1,
				"explanation": "Checks \u202bthe admin\u202c role and \u2066the owner\u2069 role.",
			}},
		}
	})

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowRunID)
	require.NoError(t, err)
	assert.Contains(t, out, `  - major, a.ts:1: Checks \u202bthe admin\u202c role and \u2066the owner\u2069 role.`+"\n")
	for _, raw := range []string{"\u202b", "\u202c", "\u2066", "\u2069"} {
		assert.NotContains(t, out, raw)
	}
}
