package lavaflow

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

// heldQuestion is how runs open asks, after what opening does.
const heldQuestion = "Opening the pull request lets every job above run this fix with the repository's secrets " +
	"before anyone reviews it on GitHub. Volcano opens it only while the diff and its jobs still match both fingerprints.\n" +
	"Open the fix's pull request? Type 'y' or 'yes' to confirm: "

// heldReview is everything runs open shows for the held run with this diff,
// as it prints it, and this fix fingerprint, as it reads on stderr.
func heldReview(diff, diffSHA256 string) string {
	return `Run: ` + lavaflowHeldID + `
Resource: function checkout
Title: Load the cart before checkout reads it
Issue: ` + lavaflowIssueID + `
Held: a workflow could run this fix with the repository's secrets before a person approves it

Diff
` + diff + `
Jobs that will get the repository's secrets (2)
  - .github/workflows/preview.yml on main: job deploy-preview, runs on pull_request_target
  - .github/workflows/broken.yml on ` + heldBaseCommit + `: couldn't be read
Workflows read at:
  - main at 3f786850e387550fdab836ed7e6dc881de23001b
  - commit ` + heldBaseCommit + `

Fix fingerprint: ` + diffSHA256 + `
Jobs fingerprint: ` + heldJobsSHA256 + `
`
}

// heldDiffShown is heldDiff as runs open prints it.
const heldDiffShown = `  --- a/functions/checkout.ts
  +++ b/functions/checkout.ts
  @@ -1,2 +1,3 @@
   export async function handleCheckout(cart) {
  +  if (!cart) return conflict()
     return charge(cart)
`

// wantOpen is the whole body of a request to open the held fix with these
// fingerprints: the project, the run and the two fingerprints.
func wantOpen(diffSHA256 string) map[string]any {
	return map[string]any{
		"project_id": lavaflowProjectID, "run_id": lavaflowHeldID, "diff_sha256": diffSHA256, "jobs_sha256": heldJobsSHA256,
	}
}

// answers gives one answer per read, as a terminal does, and calls before,
// when set, ahead of each answer it gives.
type answers struct {
	lines  []string
	before func(n int)
	n      int
}

func (a *answers) Read(p []byte) (int, error) {
	if a.n >= len(a.lines) {
		return 0, io.EOF
	}
	if a.before != nil {
		a.before(a.n)
	}
	n := copy(p, a.lines[a.n])
	a.n++
	return n, nil
}

func notATerminal(t *testing.T) io.Reader {
	t.Helper()
	stdin, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stdin.Close() })
	return stdin
}

// runs open shows the diff as served, every job that will get the
// repository's secrets and both fingerprints, asks, and queues the fix at
// POST /lavaflow/held-fix-opens with a body of exactly the project, the run
// and the two fingerprints it showed.
func TestRunsOpenShowsTheHeldFixThenOpensItWithWhatItShowed(t *testing.T) {
	fake, deps, _ := newHeldLavaflowServer(t)

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.NoError(t, err)
	assert.Equal(t, heldReview(heldDiffShown, heldDiffSHA256)+"\n"+heldQuestion, errOut)
	assert.Equal(t, "✓ LavaFlow will open the fix's pull request shortly. Run "+lavaflowHeldID+
		" is queued; follow it with 'volcano cloud lavaflow show "+lavaflowHeldID+"'\n", out)
	fake.set(func(s *lavaflowServer) {
		assert.Equal(t, []map[string]any{wantOpen(heldDiffSHA256)}, s.opens)
		assert.Len(t, s.queries["/held-fix-opens"], 1, "the open is a POST to /lavaflow/held-fix-opens")
		assert.Equal(t, []url.Values{{}}, s.queries["/held-fix-opens"], "it names the project and the run in its body only")
		assert.Equal(t, []string{lavaflowProjectID, lavaflowProjectID}, s.projects, "the read and the open name the project")
	})
	assertLavaFlowWording(t, errOut+out)
}

// --json prints the queued run as the API answered it, and what runs open
// shows and asks goes to stderr.
func TestRunsOpenJSONPrintsTheQueuedRun(t *testing.T) {
	_, deps, _ := newHeldLavaflowServer(t)

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("yes\n"), "runs", "open", lavaflowHeldID, "--json")
	require.NoError(t, err)
	assert.Equal(t, heldReview(heldDiffShown, heldDiffSHA256)+"\n"+heldQuestion, errOut)
	run := decodeJSON(t, out)
	assert.Equal(t, "queued", run["status"])
	assert.Equal(t, lavaflowHeldID, run["id"])
}

// The answer defaults to no: anything but y or yes opens nothing and exits
// cleanly.
func TestRunsOpenOpensNothingWhenThePersonDeclines(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", "", "maybe\n"} {
		fake, deps, _ := newHeldLavaflowServer(t)

		out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(answer), "runs", "open", lavaflowHeldID)
		require.NoError(t, err, "%q", answer)
		assert.Empty(t, out, "%q", answer)
		assert.Equal(t, heldReview(heldDiffShown, heldDiffSHA256)+"\n"+heldQuestion+"Cancelled.\n", errOut, "%q", answer)
		assert.Empty(t, fake.opens, "%q", answer)
	}
}

// --yes answers only when stdin isn't a terminal, and only when it is given.
// At a terminal the question is asked anyway.
func TestRunsOpenYesAnswersOnlyWithoutATerminal(t *testing.T) {
	fake, deps, _ := newHeldLavaflowServer(t)
	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), notATerminal(t), "runs", "open", lavaflowHeldID)
	require.EqualError(t, err, "stdin is not a terminal: nothing was opened; pass --yes to open the fix without the question")
	assert.Contains(t, errOut, "Fix fingerprint: "+heldDiffSHA256, "it shows what it would open")
	assert.Empty(t, fake.opens)

	fake, deps, _ = newHeldLavaflowServer(t)
	_, _, err = executeLavaflowCommandSplit(t, New(deps), notATerminal(t), "runs", "open", lavaflowHeldID, "--yes=false")
	require.ErrorContains(t, err, "stdin is not a terminal")
	assert.Empty(t, fake.opens)

	fake, deps, _ = newHeldLavaflowServer(t)
	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), notATerminal(t), "runs", "open", lavaflowHeldID, "--yes")
	require.NoError(t, err)
	assert.Equal(t, heldReview(heldDiffShown, heldDiffSHA256)+"\n"+strings.TrimSuffix(heldQuestion, "Open the fix's pull request? Type 'y' or 'yes' to confirm: "),
		errOut, "--yes still shows the diff, its jobs and the fingerprints")
	assert.Contains(t, out, "is queued")
	assert.Equal(t, []map[string]any{wantOpen(heldDiffSHA256)}, fake.opens)

	// A test's reader stands in for a terminal, as confirm.CanPrompt reads it.
	fake, deps, _ = newHeldLavaflowServer(t)
	_, errOut, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader("\n"), "runs", "open", lavaflowHeldID, "--yes")
	require.NoError(t, err)
	assert.Contains(t, errOut, "Open the fix's pull request? Type 'y' or 'yes' to confirm: Cancelled.\n")
	assert.Empty(t, fake.opens)
}

// A diff that leaves out a file that may hold a credential, or is cut short,
// isn't fully reviewable, so LavaFlow can't open it. runs open shows the diff
// with each marker highlighted, warns with the count and the paths, then
// refuses without asking or sending anything.
func TestRunsOpenRefusesADiffThatIsntFullyReviewable(t *testing.T) {
	const withheldSecret = "# LavaFlow withheld the changes to \".env.example\" because they may contain a credential"
	const withheldUnnamed = "# LavaFlow withheld the changes to a file, whose path it does not show, because they may contain a credential"
	const cut = "[diff truncated; the rest of the change is not shown]"
	const unnamed = "a file whose path LavaFlow can't show, or couldn't read back"
	const one = "Warning: 1 file withheld: it may contain credentials or is too large to show. You can't review it here.\n"
	const two = "Warning: 2 files withheld: they may contain credentials or are too large to show. You can't review them here.\n"
	const note = " withheld: it may contain credentials or is too large to show.\n"
	for name, tc := range map[string]struct {
		change   func(held map[string]any)
		shown    []string
		warnings string
	}{
		"withheld files": {
			func(held map[string]any) {
				setHeldDiff(held, heldDiff+withheldSecret+"\n"+withheldUnnamed+"\n")
				held["diff_withheld"] = []any{".env.example", ""}
			},
			[]string{"!! " + withheldSecret + "\n", "!! " + withheldUnnamed + "\n"},
			two + "!! .env.example" + note + "!! " + unnamed + "\n",
		},
		"cut short": {
			func(held map[string]any) {
				setHeldDiff(held, heldDiff+cut+"\n")
				held["diff_truncated"] = true
			},
			[]string{"!! " + cut + "\n"},
			"Warning: The diff is cut short past 256 KiB, so it doesn't show the whole fix.\n",
		},
		"cut short by the API's word alone": {func(held map[string]any) {
			held["diff_truncated"] = true
		}, nil, "Warning: The diff is cut short past 256 KiB, so it doesn't show the whole fix.\n"},
		// A run whose stored diff can't be read back is served as one marker
		// without a path, and diff_withheld [""].
		"stored diff not read back": {
			func(held map[string]any) {
				setHeldDiff(held, withheldUnnamed+"\n")
				held["diff_withheld"] = []any{""}
			},
			[]string{"!! " + withheldUnnamed + "\n"},
			one + "!! " + unnamed + "\n",
		},
		"no path in diff_withheld alone": {
			func(held map[string]any) { held["diff_withheld"] = []any{""} },
			nil,
			one + "!! " + unnamed + "\n",
		},
		"a marker line with no path": {
			func(held map[string]any) { setHeldDiff(held, heldDiff+"# LavaFlow withheld a file\n") },
			[]string{"!! # LavaFlow withheld a file\n"},
			one + "!! " + unnamed + "\n",
		},
		"withheld by the API's word alone": {func(held map[string]any) {
			held["diff_withheld"] = []any{"config/deploy.sh"}
		}, nil, one + "!! config/deploy.sh" + note},
		"the API lists more than the diff marks": {
			func(held map[string]any) {
				setHeldDiff(held, heldDiff+withheldSecret+"\n")
				held["diff_withheld"] = []any{".env.example", "fixtures/large.json"}
			},
			[]string{"!! " + withheldSecret + "\n"},
			two + "!! .env.example" + note + "!! fixtures/large.json" + note,
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps, held := newHeldLavaflowServer(t)
			fake.set(func(*lavaflowServer) { tc.change(held) })

			_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID, "--yes")
			require.ErrorIs(t, err, clilavaflow.ErrNotReviewable)
			assert.Equal(t, "LavaFlow can't open a fix whose diff isn't fully reviewable, so nothing was opened", err.Error())
			for _, line := range tc.shown {
				assert.Contains(t, errOut, line, "the marker is highlighted in the diff")
			}
			assert.Contains(t, errOut, "Jobs fingerprint: "+heldJobsSHA256+"\n\n"+tc.warnings,
				"the warning comes last, after the fingerprints")
			assert.NotContains(t, errOut, "Open the fix's pull request?")
			assert.NotContains(t, errOut, "!! \n", "no empty path is printed")
			assert.Empty(t, fake.opens, "nothing is sent")
		})
	}
}

// A run that holds no fix a person can open is refused before anything is
// shown or asked, saying why. So is a diff whose bytes aren't the ones its
// fingerprint covers, since what was shown wouldn't be what opens.
func TestRunsOpenRefusesARunThatHoldsNoFixToOpen(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(run map[string]any)
		want   string
	}{
		"opened": {func(run map[string]any) {
			run["pull_request_skipped"], run["status"] = nil, "pr_open"
			run["pull_request"] = map[string]any{"number": 43, "url": "https://github.com/acme/shop/pull/43", "state": "open", "draft": false}
		}, "LavaFlow run " + lavaflowHeldID + " already opened its fix's pull request, #43, so there's nothing to open. " +
			"See it with 'volcano cloud lavaflow show " + lavaflowHeldID + "'"},
		"running": {func(run map[string]any) {
			run["pull_request_skipped"], run["status"], run["finished_at"] = nil, "reviewing", nil
		}, "LavaFlow is still working on run " + lavaflowHeldID + ", so it holds no fix to open yet. " +
			"Follow it with 'volcano cloud lavaflow show " + lavaflowHeldID + "'"},
		"skipped for another reason": {func(run map[string]any) {
			run["pull_request_skipped"] = "possible_credential"
		}, "LavaFlow didn't hold run " + lavaflowHeldID + "'s fix for the repository's workflows, so there's nothing to open. " +
			"See why with 'volcano cloud lavaflow show " + lavaflowHeldID + "'"},
		"held before LavaFlow kept its diff": {func(run map[string]any) {
			run["diff"], run["diff_sha256"] = nil, nil
		}, "LavaFlow held run " + lavaflowHeldID + "'s fix before it kept a fix's diff to review, so it can't be opened. " +
			"See the run with 'volcano cloud lavaflow show " + lavaflowHeldID + "'"},
		"jobs not listed": {func(run map[string]any) {
			run["held_workflows"] = nil
		}, "LavaFlow couldn't list every workflow job that held run " + lavaflowHeldID + "'s fix, as when there are " +
			"more than 256, so it can't be opened. Gate those workflows on a label a person adds instead"},
		"fingerprint the CLI can't check": {func(run map[string]any) {
			run["diff_sha256"] = strings.ToUpper(heldDiffSHA256)
		}, "the fingerprints Volcano sent for run " + lavaflowHeldID + " aren't in a form this CLI can check, so nothing was opened"},
		"diff that isn't the one fingerprinted": {func(run map[string]any) {
			run["diff"] = heldDiff + "+  audit(cart)\n"
		}, "the diff Volcano sent for run " + lavaflowHeldID + " doesn't match its fix fingerprint, so nothing was opened. " +
			"Run this again"},
	} {
		fake, deps, held := newHeldLavaflowServer(t)
		fake.set(func(*lavaflowServer) { tc.change(held) })

		_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
		require.EqualError(t, err, tc.want, name)
		assertNothingShownOrAsked(t, errOut, name)
		assert.Empty(t, fake.opens, name)
	}
}

// When the diff or its jobs changed before Volcano opened the fix, runs open
// reads the run again, shows the new diff, jobs and fingerprints, and asks
// again; the open sends the new fingerprints.
func TestRunsOpenShowsAChangedFixAgainAndAsksAgain(t *testing.T) {
	const changed = heldDiff + "+  audit(cart)\n"
	fake, deps, held := newHeldLavaflowServer(t)
	stdin := &answers{lines: []string{"y\n", "y\n"}, before: func(n int) {
		if n == 0 {
			fake.set(func(*lavaflowServer) { setHeldDiff(held, changed) })
		}
	}}

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), stdin, "runs", "open", lavaflowHeldID)
	require.NoError(t, err)
	assert.Contains(t, out, "is queued")
	assert.Equal(t, heldReview(heldDiffShown, heldDiffSHA256)+"\n"+heldQuestion+
		"Warning: The fix or its jobs changed since you looked. Read them again:\n\n"+
		heldReview(heldDiffShown+"  +  audit(cart)\n", sha256Hex(changed))+"\n"+heldQuestion, errOut)
	fake.set(func(s *lavaflowServer) {
		assert.Equal(t, []map[string]any{wantOpen(heldDiffSHA256), wantOpen(sha256Hex(changed))}, s.opens,
			"each open sent the fingerprints shown just before it")
	})
}

// A fix that changes every time it's shown stops after three rounds.
func TestRunsOpenStopsAFixThatKeepsChanging(t *testing.T) {
	fake, deps, _ := newHeldLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusConflict, http.MethodPost
		s.refusal = map[string]any{"error": "the fix or its jobs changed", "code": "run_changed"}
	})

	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), &answers{lines: []string{"y\n", "y\n", "y\n", "y\n"}},
		"runs", "open", lavaflowHeldID)
	require.ErrorIs(t, err, clilavaflow.ErrFixKeptChanging)
	assert.Equal(t, "the fix or its jobs changed each of the 3 times you looked, so nothing was opened; run it again later", err.Error())
	assert.Equal(t, http.StatusConflict, api.Status(err))
	assert.Equal(t, 3, strings.Count(errOut, "Open the fix's pull request?"))
	assert.Len(t, fake.opens, 3)
}

// --yes without a terminal answered for the fix first shown, so a change
// ends the command rather than open a fix no one read.
func TestRunsOpenWithYesStopsWhenTheFixChanged(t *testing.T) {
	fake, deps, _ := newHeldLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusConflict, http.MethodPost
		s.refusal = map[string]any{"error": "the fix or its jobs changed", "code": "run_changed"}
	})

	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), notATerminal(t), "runs", "open", lavaflowHeldID, "--yes")
	require.ErrorIs(t, err, clilavaflow.ErrFixChanged)
	assert.Equal(t, "the fix or its jobs changed since you looked; run it again", err.Error())
	assert.Equal(t, 1, strings.Count(errOut, "Fix fingerprint:"), "shown once")
	assert.Len(t, fake.opens, 1)
	assert.Len(t, fake.queries["/runs/"+lavaflowHeldID], 1, "the run is read once")
}

// Every other 409 says plainly why LavaFlow can't open the fix, as runs start
// says the codes they share, and keeps the 409 for the exit code. A code this
// CLI doesn't know keeps Volcano's words.
func TestRunsOpenSaysWhyVolcanoCantOpenIt(t *testing.T) {
	show := "'volcano cloud lavaflow show " + lavaflowHeldID + "'"
	for code, want := range map[string]string{
		"run_not_held":      "LavaFlow run " + lavaflowHeldID + " no longer holds a fix a person can open. See it with " + show,
		"lavaflow_disabled": "LavaFlow is off for this project. Turn it on with 'volcano cloud lavaflow enable', then run this again",
		"repository_required": "the project no longer links the repository LavaFlow was turned on for. " +
			"Connect it again with 'volcano git connect', or turn LavaFlow on again with 'volcano cloud lavaflow enable'",
		"repository_changed": "the project links another repository than LavaFlow was turned on for, or than this fix " +
			"was written for, so LavaFlow can't open it. Turn LavaFlow on again with 'volcano cloud lavaflow enable' " +
			"to fix issues in the one it links now",
		"repository_unreadable": "LavaFlow can no longer read the repository's workflows, so it can't open the fix. " +
			"Connect the repository again with 'volcano git connect', then run this again",
		"lavaflow_run_in_progress": "another LavaFlow run is in progress for this issue, so LavaFlow can't open this fix now. " +
			"Try again once it finishes",
		"run_attempts_exhausted": "LavaFlow has used every attempt it gets for run " + lavaflowHeldID +
			", so it won't open the fix. See the run with " + show,
		"run_moved": `LavaFlow didn't open the fix. Volcano said: "refused"`,
	} {
		fake, deps, _ := newHeldLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = http.StatusConflict, http.MethodPost
			s.refusal = map[string]any{"error": "refused", "code": code}
		})

		_, _, err := executeLavaflowCommandSplit(t, New(deps), &answers{lines: []string{"y\n", "y\n"}}, "runs", "open", lavaflowHeldID)
		require.EqualError(t, err, want, code)
		assert.Equal(t, http.StatusConflict, api.Status(err), code)
		assert.Len(t, fake.opens, 1, "%s: asked once, sent once", code)
	}
}

// A 429 or 503 says when to try again, from Retry-After, and nothing is
// retried for the person.
func TestRunsOpenSaysWhenToTryAgain(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		for retryAfter, when := range map[string]string{"30": "Try again in 30s", "": "Try again later"} {
			fake, deps, _ := newHeldLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.refuse, s.refuseMethod = status, http.MethodPost
				s.refusal = map[string]any{"error": "too many opens"}
				if retryAfter != "" {
					s.refusalHeaders = map[string]string{"Retry-After": retryAfter}
				}
			})

			_, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
			require.EqualError(t, err, "LavaFlow didn't open the fix: Volcano can't take the request now (too many opens). "+when,
				"%d %q", status, retryAfter)
			assert.Equal(t, status, api.Status(err))
			assert.Len(t, fake.opens, 1, "%d %q: not retried", status, retryAfter)
		}
	}
}

// Volcano refuses a credential that isn't the owner's live sign-in, or an
// owner whose GitHub account can't push to the repository, with
// person_required. The CLI says what the open needs, keeps Volcano's words,
// which name what to change, and points at volcano login, with the exit code
// of a permission refused.
func TestRunsOpenSaysAPersonRequiredRefusalNeedsTheOwnersLiveSignIn(t *testing.T) {
	const needs = "opening a fix LavaFlow held needs the project owner's live sign-in, " +
		"and a GitHub account that can push to the project's repository. "
	const login = "Run 'volcano login', or use a platform token that expires"
	const neverExpires = "only the project's owner, signed in with `volcano login` or a platform token that expires, " +
		"can open a fix LavaFlow held"
	const cantPush = "give the project owner's GitHub account push access to the project's repository, then open the fix"
	for name, tc := range map[string]struct {
		message  string
		envToken bool
		want     string
	}{
		"a token that never expires": {neverExpires, false, needs + "Volcano said: \"" + neverExpires + "\". " + login},
		"an owner who can't push":    {cantPush, false, needs + "Volcano said: \"" + cantPush + "\". " + login},
		"no words":                   {"", false, needs + login},
		"token in VOLCANO_TOKEN": {neverExpires, true, needs + "Volcano said: \"" + neverExpires + "\". " +
			"Unset VOLCANO_TOKEN and run 'volcano login', or set it to a platform token that expires"},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps, _ := newHeldLavaflowServer(t)
			if tc.envToken {
				t.Setenv("VOLCANO_TOKEN", "pk-secret")
			}
			fake.set(func(s *lavaflowServer) {
				s.refuse, s.refuseMethod = http.StatusForbidden, http.MethodPost
				s.refusal = map[string]any{"error": tc.message, "code": "person_required"}
			})

			_, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
			require.EqualError(t, err, tc.want)
			require.ErrorIs(t, err, clilavaflow.ErrOpenNeedsLiveSignIn)
			assert.Equal(t, http.StatusForbidden, api.Status(err))
		})
	}
}

// A 400 or 413 means Volcano didn't take the request this CLI built: the
// command says nothing was queued, keeps Volcano's words, and points at
// volcano upgrade, without retrying. A 401 keeps its status, so the exit code
// says the credential isn't signed in, and keeps Volcano's words.
func TestRunsOpenSaysWhenVolcanoRefusesTheRequest(t *testing.T) {
	const upgrade = "Update the CLI with 'volcano upgrade' and run this again"
	for _, tc := range []struct {
		status        int
		message, want string
	}{
		{
			http.StatusBadRequest, "jobs_sha256 must be 64 lowercase hex characters",
			clilavaflow.ErrOpenRequestRefused.Error() + `. Volcano said: "jobs_sha256 must be 64 lowercase hex characters". ` + upgrade,
		},
		{
			http.StatusRequestEntityTooLarge, "Request body exceeds 4,096 bytes.",
			clilavaflow.ErrOpenRequestRefused.Error() + `. Volcano said: "Request body exceeds 4,096 bytes". ` + upgrade,
		},
		{http.StatusBadRequest, "", clilavaflow.ErrOpenRequestRefused.Error() + ". " + upgrade},
	} {
		fake, deps, _ := newHeldLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = tc.status, http.MethodPost
			s.refusal = map[string]any{"error": tc.message}
		})

		_, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
		require.EqualError(t, err, tc.want, "%d %q", tc.status, tc.message)
		require.ErrorIs(t, err, clilavaflow.ErrOpenRequestRefused)
		assert.Equal(t, tc.status, api.Status(err))
		assert.Len(t, fake.opens, 1, "%d: not retried", tc.status)
	}

	fake, deps, _ := newHeldLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusUnauthorized, http.MethodPost
		s.refusal = map[string]any{"error": "invalid or expired token"}
	})
	_, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.ErrorContains(t, err, "invalid or expired token")
	require.NotErrorIs(t, err, clilavaflow.ErrOpenRequestRefused)
	assert.Equal(t, http.StatusUnauthorized, api.Status(err))
	assert.Len(t, fake.opens, 1)
}

// The owner's plan, and an owner whose account is read-only or inactive, are
// refused with 403 too, each said for what it is.
func TestRunsOpenSaysWhenThePlanOrCredentialCantOpenIt(t *testing.T) {
	for name, tc := range map[string]struct {
		refusal map[string]any
		want    string
	}{
		"plan": {map[string]any{"error": "LavaFlow needs a plan that includes it", "code": "plan_required"}, lavaflowPlanRequired},
		"read-only or inactive account": {
			map[string]any{"error": "the project's owner is read-only or inactive"},
			`LavaFlow didn't open the fix: a read-only or inactive account can't open one, and otherwise the project ` +
				`owner's plan couldn't be read; try again later. Volcano said: "the project's owner is read-only or inactive"`,
		},
		"no words": {map[string]any{"error": ""}, clilavaflow.ErrOpenRefused.Error()},
	} {
		fake, deps, _ := newHeldLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod, s.refusal = http.StatusForbidden, http.MethodPost, tc.refusal
		})

		_, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
		require.EqualError(t, err, tc.want, name)
		assert.Equal(t, http.StatusForbidden, api.Status(err), name)
	}
}

// Opening a held fix needs the owner's own session, so a project access token
// is refused before anything is sent, as runs start refuses one, with the
// exit code of a permission the API refuses.
func TestRunsOpenRefusesAProjectAccessTokenBeforeAnything(t *testing.T) {
	for name, tc := range map[string]struct {
		token func(t *testing.T)
		how   string
	}{
		"from the environment": {func(t *testing.T) {
			t.Helper()
			t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")
		}, "Unset VOLCANO_TOKEN and sign in with 'volcano login', then run this again"},
		"saved": {func(t *testing.T) {
			t.Helper()
			require.NoError(t, (&cliconfig.Config{
				UserToken:      cliconfig.ProjectTokenPrefix + "secret",
				CurrentProject: &cliconfig.ProjectConfig{ID: lavaflowProjectID, Name: "Shop"},
			}).Save())
		}, "Sign in with 'volcano login' and run this again"},
	} {
		t.Run(name, func(t *testing.T) {
			fake, deps, _ := newHeldLavaflowServer(t)
			tc.token(t)

			_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID, "--yes")
			require.EqualError(t, err, "opening a fix LavaFlow held needs the project owner's session: "+
				"a project access token can't. "+tc.how)
			require.ErrorIs(t, err, clilavaflow.ErrPersonOpens)
			require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired, "the exit code reports a permission refused")
			assertNothingShownOrAsked(t, errOut, name)
			fake.set(func(s *lavaflowServer) {
				assert.Empty(t, s.queries, "nothing reached the API")
				assert.Empty(t, s.opens)
			})
		})
	}
}

// A run the project doesn't have, or one that went away before the open, is
// named as missing with the exit code of a missing resource.
func TestRunsOpenSaysWhenTheRunIsUnknown(t *testing.T) {
	const unknown = "12121212-1212-4212-8212-121212121212"
	fake, deps, _ := newHeldLavaflowServer(t)
	_, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", unknown)
	require.EqualError(t, err, "no LavaFlow run "+unknown+" in this project: find one with 'volcano cloud lavaflow list'")
	assert.Equal(t, http.StatusNotFound, api.Status(err))
	assert.Empty(t, fake.opens)

	fake, deps, _ = newHeldLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusNotFound, http.MethodPost
		s.refusal = map[string]any{"error": "run not found"}
	})
	_, _, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.EqualError(t, err, "no LavaFlow run "+lavaflowHeldID+" in this project: find one with 'volcano cloud lavaflow list'")
	assert.Equal(t, http.StatusNotFound, api.Status(err))

	_, _, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", "latest")
	require.EqualError(t, err, `invalid run ID "latest": use an ID from 'volcano cloud lavaflow list'`)
}

// Where LavaFlow isn't available, outside its cohort or with its release
// gate off, runs open says it isn't available before it shows or asks
// anything, as every LavaFlow command does, with the exit code of the 404.
// So does the open's own feature_unavailable. An API that has LavaFlow's runs
// but can't open a held fix says that.
func TestRunsOpenSaysWhenItCantOpenHere(t *testing.T) {
	fake, deps, _ := newHeldLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusNotFound
		s.refusal = map[string]any{"error": "LavaFlow is not available", "code": "feature_unavailable"}
	})
	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.EqualError(t, err, lavaflowNotAvailable)
	assert.Equal(t, http.StatusNotFound, api.Status(err))
	assertNothingShownOrAsked(t, errOut, "unavailable")

	fake, deps, _ = newHeldLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusNotFound, http.MethodPost
		s.refusal = map[string]any{"error": "LavaFlow is not available", "code": "feature_unavailable"}
	})
	_, _, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.EqualError(t, err, lavaflowNotAvailable, "the open's own feature_unavailable")
	assert.Equal(t, http.StatusNotFound, api.Status(err))
	assert.Len(t, fake.opens, 1)

	fake, deps, _ = newHeldLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.openRouteMissing = true })
	_, _, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.ErrorIs(t, err, clilavaflow.ErrOpenUnsupported)
	assert.Len(t, fake.opens, 1, "the open reached POST /lavaflow/held-fix-opens")
	assert.Equal(t, "this Volcano API can't open a fix LavaFlow held yet", err.Error())
}

// Text from the run prints as plain text: a control character in the diff or
// a job prints as a visible escape.
func TestRunsOpenPrintsRunTextAsPlainText(t *testing.T) {
	fake, deps, held := newHeldLavaflowServer(t)
	fake.set(func(*lavaflowServer) {
		setHeldDiff(held, heldDiff+"+  log(\"\x1b[31mred\")\n")
		jobs := held["held_workflows"].(map[string]any)["jobs"].([]any)
		jobs[0].(map[string]any)["job"] = "deploy\x1b]0;title\x07"
	})

	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("n\n"), "runs", "open", lavaflowHeldID)
	require.NoError(t, err)
	assert.Contains(t, errOut, "  +  log(\"\\x1b[31mred\")\n")
	assert.Contains(t, errOut, "job deploy\\x1b]0;title\\x07, runs on pull_request_target")
	for _, raw := range []string{"\x1b", "\x07"} {
		assert.NotContains(t, errOut, raw)
	}
}

// The fix fingerprint is checked against the diff exactly as decoded from the
// API, before display escapes a control character or a carriage return, and
// the open sends the served fingerprint. A fingerprint of the diff as
// displayed, or with its line endings changed, is refused.
func TestRunsOpenChecksTheFingerprintAgainstTheDiffAsServed(t *testing.T) {
	const served = "--- a/functions/checkout.ts\r\n+++ b/functions/checkout.ts\r\n@@ -1,2 +1,3 @@\r\n" +
		" export async function handleCheckout(cart) {\r\n+  log(\"\x1b[31mred\x07\")\r\n   return charge(cart)\r\n"
	fake, deps, held := newHeldLavaflowServer(t)
	fake.set(func(*lavaflowServer) { setHeldDiff(held, served) })

	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.NoError(t, err)
	assert.Contains(t, errOut, "  +  log(\"\\x1b[31mred\\x07\")\\x0d\n", "display escapes what the fingerprint covers raw")
	assert.Contains(t, errOut, "Fix fingerprint: "+sha256Hex(served)+"\n")
	assert.Equal(t, []map[string]any{wantOpen(sha256Hex(served))}, fake.opens, "the open sends the served fingerprint")

	displayed := strings.NewReplacer("\r", "\\x0d", "\x1b", "\\x1b", "\x07", "\\x07").Replace(served)
	for name, fingerprint := range map[string]string{
		"as displayed":         sha256Hex(displayed),
		"with LF line endings": sha256Hex(strings.ReplaceAll(served, "\r\n", "\n")),
	} {
		fake, deps, held := newHeldLavaflowServer(t)
		fake.set(func(*lavaflowServer) {
			held["diff"], held["diff_sha256"] = served, fingerprint
		})

		_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
		require.EqualError(t, err, "the diff Volcano sent for run "+lavaflowHeldID+" doesn't match its fix fingerprint, "+
			"so nothing was opened. Run this again", name)
		assertNothingShownOrAsked(t, errOut, name)
		assert.Empty(t, fake.opens, name)
	}
}

// show names runs open under a run whose held fix a person can open, and
// only there.
func TestShowNamesRunsOpenForAHeldFix(t *testing.T) {
	const hint = "To read this fix and open its pull request yourself, run volcano cloud lavaflow runs open "
	fake, deps, held := newHeldLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "show", lavaflowHeldID)
	require.NoError(t, err)
	assert.Contains(t, out, "\n"+hint+lavaflowHeldID+"\n")

	for _, id := range []string{lavaflowRunID, lavaflowFailedID} {
		out, err = executeLavaflowCommand(t, New(deps), "show", id)
		require.NoError(t, err)
		assert.NotContains(t, out, hint, id)
	}

	for name, change := range map[string]func(){
		"jobs not listed": func() { held["held_workflows"] = nil },
		"withheld file":   func() { held["diff_withheld"] = []any{".env"} },
	} {
		fake.set(func(*lavaflowServer) { change() })
		out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowHeldID)
		require.NoError(t, err)
		assert.NotContains(t, out, hint, name)
	}
}

// assertNothingShownOrAsked checks a refused runs open showed no fix and
// asked nothing.
func assertNothingShownOrAsked(t *testing.T, errOut, name string) {
	t.Helper()
	for _, shown := range []string{"Diff\n", "Fix fingerprint:", "Open the fix's pull request?"} {
		assert.NotContains(t, errOut, shown, name)
	}
}
