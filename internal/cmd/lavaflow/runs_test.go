package lavaflow

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

// heldReview is everything runs open shows for the held run before it asks,
// as it reads on stderr.
const heldReview = `Run: ` + lavaflowHeldID + `
Resource: function checkout
Title: Load the cart before checkout reads it
Issue: ` + lavaflowIssueID + `
Held: a workflow could run this fix with the repository's secrets before a person approves it

Diff
  --- a/functions/checkout.ts
  +++ b/functions/checkout.ts
  @@ -1,2 +1,3 @@
   export async function handleCheckout(cart) {
  +  if (!cart) return conflict()
     return charge(cart)

Jobs that will get the repository's secrets (2)
  - .github/workflows/preview.yml on main: job deploy-preview, runs on pull_request_target
  - .github/workflows/broken.yml on main: couldn't be read
Workflows read at:
  - main at 3f786850e387550fdab836ed7e6dc881de23001b
  - commit 4f1c2b3d5e6f708192a3b4c5d6e7f8091a2b3c4d

Fix fingerprint: ` + heldDiffSHA256 + `
Jobs fingerprint: ` + heldJobsSHA256 + `

Opening the pull request lets every job above run this fix with the repository's secrets before anyone reviews it on GitHub. Volcano opens it only while the fix and its jobs still match both fingerprints.
Open the fix's pull request? Type 'y' or 'yes' to confirm: `

// runs open shows the fix, every job that will get the repository's secrets
// and both fingerprints, asks, and opens the fix with exactly the
// fingerprints it showed.
func TestRunsOpenShowsTheHeldFixThenOpensItWithWhatItShowed(t *testing.T) {
	fake, deps, _ := newHeldLavaflowServer(t)

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.NoError(t, err)
	assert.Equal(t, heldReview, errOut)
	assert.Equal(t, "✓ LavaFlow opened the fix's pull request: #43 https://github.com/acme/shop/pull/43\n", out)
	fake.set(func(s *lavaflowServer) {
		assert.Equal(t, []map[string]any{{"diff_sha256": heldDiffSHA256, "jobs_sha256": heldJobsSHA256}}, s.opens)
		assert.Equal(t, []string{lavaflowProjectID, lavaflowProjectID}, s.projects, "the read and the open name the project")
	})
	assertLavaFlowWording(t, errOut+out)
}

// --json prints the run as the API answered the open, and what runs open
// shows and asks goes to stderr.
func TestRunsOpenJSONPrintsTheRunTheAPIAnswered(t *testing.T) {
	_, deps, _ := newHeldLavaflowServer(t)

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("yes\n"), "runs", "open", lavaflowHeldID, "--json")
	require.NoError(t, err)
	assert.Equal(t, heldReview, errOut)
	run := decodeJSON(t, out)
	assert.Equal(t, "pr_open", run["status"])
	assert.Equal(t, "https://github.com/acme/shop/pull/43", run["pull_request"].(map[string]any)["url"])
}

// The answer defaults to no: anything but y or yes opens nothing and exits
// cleanly.
func TestRunsOpenOpensNothingWhenThePersonDeclines(t *testing.T) {
	for _, answer := range []string{"n\n", "\n", "", "maybe\n"} {
		fake, deps, _ := newHeldLavaflowServer(t)

		out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(answer), "runs", "open", lavaflowHeldID)
		require.NoError(t, err, "%q", answer)
		assert.Empty(t, out, "%q", answer)
		assert.Equal(t, heldReview+"Cancelled.\n", errOut, "%q", answer)
		assert.Empty(t, fake.opens, "%q", answer)
	}
}

// --yes answers only when stdin isn't a terminal, and only when it is given.
// At a terminal the question is asked anyway.
func TestRunsOpenYesAnswersOnlyWithoutATerminal(t *testing.T) {
	notATerminal := func(t *testing.T) io.Reader {
		t.Helper()
		stdin, err := os.Open(os.DevNull)
		require.NoError(t, err)
		t.Cleanup(func() { _ = stdin.Close() })
		return stdin
	}

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
	assert.Equal(t, strings.TrimSuffix(heldReview, "Open the fix's pull request? Type 'y' or 'yes' to confirm: "), errOut,
		"--yes still shows the fix, its jobs and the fingerprints")
	assert.Contains(t, out, "#43")
	assert.Equal(t, []map[string]any{{"diff_sha256": heldDiffSHA256, "jobs_sha256": heldJobsSHA256}}, fake.opens)

	// A test's reader stands in for a terminal, as confirm.CanPrompt reads it.
	fake, deps, _ = newHeldLavaflowServer(t)
	_, errOut, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader("\n"), "runs", "open", lavaflowHeldID, "--yes")
	require.NoError(t, err)
	assert.Contains(t, errOut, "Open the fix's pull request? Type 'y' or 'yes' to confirm: Cancelled.\n")
	assert.Empty(t, fake.opens)
}

// A run that holds no fix a person can open is refused before anything is
// asked, saying why.
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
	} {
		fake, deps, held := newHeldLavaflowServer(t)
		fake.set(func(*lavaflowServer) { tc.change(held) })

		_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
		require.EqualError(t, err, tc.want, name)
		assertNothingShownOrAsked(t, errOut, name)
		assert.Empty(t, fake.opens, name)
	}
}

// Volcano opens the fix only for the fingerprints the person was shown. When
// either changed, or the run no longer holds a fix, the CLI says so and keeps
// the 409 for the exit code. A 409 code this CLI doesn't know reads as a
// change: running it again shows the run as it is.
func TestRunsOpenSaysWhenTheFixChangedOrIsNoLongerHeld(t *testing.T) {
	for code, want := range map[string]string{
		"run_changed": "the fix or its jobs changed since you looked; run it again",
		"run_moved":   "the fix or its jobs changed since you looked; run it again",
		"run_not_held": "LavaFlow run " + lavaflowHeldID + " no longer holds a fix to open. " +
			"See it with 'volcano cloud lavaflow show " + lavaflowHeldID + "'",
	} {
		fake, deps, _ := newHeldLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = http.StatusConflict, http.MethodPost
			s.refusal = map[string]any{"error": "refused", "code": code}
		})

		_, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
		require.EqualError(t, err, want, code)
		assert.Equal(t, http.StatusConflict, api.Status(err), code)
	}

	// The fake API checks the fingerprints itself: a job listed after the
	// read changes the run's jobs fingerprint, and the open is refused.
	fake, deps, held := newHeldLavaflowServer(t)
	_, _, err := executeLavaflowCommandSplit(t, New(deps), &changeOnRead{
		Reader: strings.NewReader("y\n"),
		change: func() {
			fake.set(func(*lavaflowServer) {
				held["held_workflows"].(map[string]any)["jobs_sha256"] = strings.Repeat("0", 64)
			})
		},
	}, "runs", "open", lavaflowHeldID)
	require.ErrorIs(t, err, clilavaflow.ErrFixChanged)
	assert.Equal(t, []map[string]any{{"diff_sha256": heldDiffSHA256, "jobs_sha256": heldJobsSHA256}}, fake.opens,
		"the open sent what was shown, not what changed")
}

// changeOnRead runs change when the person answers, as if the run changed
// while they read it.
type changeOnRead struct {
	io.Reader
	change func()
}

func (r *changeOnRead) Read(p []byte) (int, error) {
	if r.change != nil {
		r.change()
		r.change = nil
	}
	return r.Reader.Read(p)
}

// Volcano refuses a credential that isn't a person's own sign-in with
// person_required, in words that say what to do. The CLI keeps them and
// points at volcano login, with the exit code of a permission refused.
func TestRunsOpenPointsAPersonRequiredRefusalAtVolcanoLogin(t *testing.T) {
	const neverExpires = "this token never expires; sign in with `volcano login` to open a held fix"
	for name, tc := range map[string]struct {
		message  string
		envToken bool
		want     string
	}{
		"Volcano's words":      {neverExpires, false, "LavaFlow didn't open the fix: " + neverExpires},
		"words from elsewhere": {"MCP can't open a held fix.", false, "LavaFlow didn't open the fix: MCP can't open a held fix. Sign in with 'volcano login' and run this again"},
		"no words":             {"", false, "opening a fix LavaFlow held needs a person's own sign-in. Sign in with 'volcano login' and run this again"},
		"token in VOLCANO_TOKEN": {neverExpires, true, "LavaFlow didn't open the fix: " + neverExpires +
			". VOLCANO_TOKEN is set, and its token is used instead of a 'volcano login' sign-in until you unset it"},
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
			assert.Equal(t, http.StatusForbidden, api.Status(err))
		})
	}
}

// Opening a held fix is a person's decision, so a project access token is
// refused before anything is sent, as a permission the API refuses.
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
			require.EqualError(t, err, "opening a fix LavaFlow held is a person's decision, and a project access token "+
				"can't make it. "+tc.how)
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

// Where LavaFlow isn't available, runs open says why before it shows or asks
// anything, as every LavaFlow command does. An API that has LavaFlow's runs
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
	fake.set(func(s *lavaflowServer) { s.openRouteMissing = true })
	_, _, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), "runs", "open", lavaflowHeldID)
	require.ErrorIs(t, err, clilavaflow.ErrOpenUnsupported)
	assert.Equal(t, "this Volcano API can't open a fix LavaFlow held yet", err.Error())
}

// The diff leaves out a file's changes that may hold a credential, and is cut
// short past 256 KiB. runs open says so before it asks, since opening opens
// all of the fix. Text from the run prints as plain text.
func TestRunsOpenSaysWhatTheDiffDoesntShow(t *testing.T) {
	fake, deps, held := newHeldLavaflowServer(t)
	fake.set(func(*lavaflowServer) {
		held["diff"] = heldDiff +
			"# LavaFlow left out a file's changes here because they may contain a credential\n" +
			"--- a/.env.example\n+++ b/.env.example\n@@ -0,0 +1 @@\n+MODE=\x1b[31mred\n" +
			"[diff truncated; the rest of the change is not shown]\n"
		jobs := held["held_workflows"].(map[string]any)["jobs"].([]any)
		jobs[0].(map[string]any)["job"] = "deploy\x1b]0;title\x07"
	})

	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("n\n"), "runs", "open", lavaflowHeldID)
	require.NoError(t, err)
	assert.Contains(t, errOut, "  +MODE=\\x1b[31mred\n")
	assert.Contains(t, errOut, "job deploy\\x1b]0;title\\x07, runs on pull_request_target")
	assert.Contains(t, errOut, "Warning: 1 file(s)' changes aren't shown above because they may contain a credential. "+
		"Opening the fix opens them too.\n")
	assert.Contains(t, errOut, "Warning: The diff above is cut short past 256 KiB. Opening the fix opens the rest too.\n")
	for _, raw := range []string{"\x1b", "\x07"} {
		assert.NotContains(t, errOut, raw)
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

	fake.set(func(*lavaflowServer) { held["held_workflows"] = nil })
	out, err = executeLavaflowCommand(t, New(deps), "show", lavaflowHeldID)
	require.NoError(t, err)
	assert.NotContains(t, out, hint, "a held fix whose jobs LavaFlow couldn't list can't be opened")
}
