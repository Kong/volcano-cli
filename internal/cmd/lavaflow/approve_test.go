package lavaflow

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

// approve shows the run, its pull request and why its merge waits, asks, and
// approves only on a yes, at the head the run reported.
func TestApproveAsksThenApproves(t *testing.T) {
	for answer, wantApproved := range map[string]bool{"y\n": true, "n\n": false} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.waitForApproval(approvalReason("sensitive_path", "Merge a change to the auth configuration?", "volcano/auth.yaml"))
		})

		out, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(answer), "approve", lavaflowRunID)
		require.NoError(t, err, "%q", answer)
		assert.Contains(t, out, "LavaFlow run "+lavaflowRunID+": "+lavaflowIssueTitle, "%q", answer)
		assert.Contains(t, out, "Pull request: https://github.com/acme/shop/pull/42", "%q", answer)
		assert.Contains(t, out, "  - Merge a change to the auth configuration? (volcano/auth.yaml)", "%q", answer)
		assert.Contains(t, out, "To stop the merge instead, close the pull request.", "%q", answer)
		assert.Contains(t, out, "Approve this merge?", "%q", answer)
		if !wantApproved {
			fake.set(func(s *lavaflowServer) { assert.Empty(t, s.approvals) })
			assert.NotContains(t, out, "Approved")
			continue
		}
		fake.set(func(s *lavaflowServer) {
			assert.Equal(t, []map[string]any{{"run_id": lavaflowRunID, "head_sha": lavaflowHeadSHA}}, s.approvals)
		})
		assert.Contains(t, out, "Approved: LavaFlow merges the fix once its required checks pass")
		assert.Contains(t, out, "Merge: waiting for CI")
	}
}

// --yes skips the question, and --json keeps stdout to the run, with the
// question, when asked, on stderr.
func TestApproveWithYesAndJSON(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.waitForApproval(approvalReason("automatic_run", nil)) })

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"approve", lavaflowRunID, "--yes", "--json")
	require.NoError(t, err)
	assert.Empty(t, errOut)
	assert.Equal(t, "waiting_for_checks", decodeJSON(t, out)["merge"].(map[string]any)["status"])

	fake, deps = newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.waitForApproval(approvalReason("automatic_run", nil)) })
	out, errOut, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"),
		"approve", lavaflowRunID, "--json")
	require.NoError(t, err)
	assert.Contains(t, errOut, "Approve this merge?")
	assert.Equal(t, "waiting_for_checks", decodeJSON(t, out)["merge"].(map[string]any)["status"])
}

// Whether a fix merges is a person's decision: a project access token is
// refused before anything is read or sent.
func TestApproveWithAProjectTokenNeedsAPersonsSession(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.waitForApproval(approvalReason("automatic_run", nil)) })
	t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")

	_, err := executeLavaflowCommand(t, New(deps), "approve", lavaflowRunID, "--yes")
	require.Error(t, err)
	assert.Equal(t, "approving a LavaFlow merge needs a person's session: a project access token can't. "+
		"Unset VOLCANO_TOKEN and sign in with 'volcano login', then run this again", err.Error())
	require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired, "the exit code reports a permission refused")
	fake.set(func(s *lavaflowServer) {
		assert.Empty(t, s.queries)
		assert.Empty(t, s.approvals)
	})
}

// A run whose merge doesn't wait for a person is refused before anything is
// asked or sent.
func TestApproveRefusesARunThatIsntWaiting(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	_, err := executeLavaflowCommand(t, New(deps), "approve", lavaflowRunID, "--yes")
	require.EqualError(t, err, "LavaFlow run "+lavaflowRunID+" isn't waiting for approval")
	assert.Equal(t, http.StatusConflict, api.Status(err))

	fake.set(func(s *lavaflowServer) {
		s.waitForApproval()
		s.runs[0]["merge"].(map[string]any)["status"] = "merged"
	})
	_, err = executeLavaflowCommand(t, New(deps), "approve", lavaflowRunID, "--yes")
	require.EqualError(t, err, "LavaFlow run "+lavaflowRunID+"'s fix was already merged")
	assert.Equal(t, http.StatusConflict, api.Status(err))
	fake.set(func(s *lavaflowServer) { assert.Empty(t, s.approvals) })
}

// A pull request that changed after the run was read isn't approved: the
// person reads it again first.
func TestApproveRefusesAPullRequestThatChanged(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.waitForApproval(approvalReason("automatic_run", nil)) })
	in := &headMovesOnAnswer{fake: fake, answer: strings.NewReader("y\n")}

	_, _, err := executeLavaflowCommandSplit(t, New(deps), in, "approve", lavaflowRunID)
	require.EqualError(t, err, "the fix's pull request changed since the run was read: check it again with "+
		"'volcano cloud lavaflow show "+lavaflowRunID+"', then approve it")
	assert.Equal(t, http.StatusConflict, api.Status(err))
	fake.set(func(s *lavaflowServer) { assert.Empty(t, s.approvals) })
}

// headMovesOnAnswer moves the pull request's head while the person answers.
type headMovesOnAnswer struct {
	fake   *lavaflowServer
	answer *strings.Reader
}

func (r *headMovesOnAnswer) Read(p []byte) (int, error) {
	r.fake.set(func(s *lavaflowServer) { s.runs[0]["merge"].(map[string]any)["head_sha"] = "0000000" })
	return r.answer.Read(p)
}

// The API's refusal of an approval is read by its code; anything else keeps
// the API's words.
func TestARefusedApprovalSaysWhy(t *testing.T) {
	for name, tc := range map[string]struct {
		refuse func(*lavaflowServer)
		want   string
		status int
	}{
		"person required": {
			refusal(http.StatusForbidden, "a person decides", "person_required"),
			"approving a LavaFlow merge needs a person's session", http.StatusForbidden,
		},
		"not the owner": {
			refusal(http.StatusForbidden, "forbidden", ""),
			"only the project's owner can approve a LavaFlow merge", http.StatusForbidden,
		},
		"owner can't push": {
			refusal(http.StatusForbidden, "the owner's GitHub connection needs reconnecting", "permission_missing"),
			"LavaFlow can't merge for this project: the project owner's GitHub account can't push to its repository " +
				"(the owner's GitHub connection needs reconnecting)", http.StatusForbidden,
		},
		"merging off": {
			refusal(http.StatusNotFound, "LavaFlow merging is not available", "feature_unavailable"),
			"LavaFlow merging isn't available for this project yet", http.StatusNotFound,
		},
		"plan": {
			refusal(http.StatusForbidden, "upgrade", "plan_required"),
			"LavaFlow needs a plan that includes it", http.StatusForbidden,
		},
		"not waiting": {
			refusal(http.StatusConflict, "not waiting", "not_waiting_for_approval"),
			"LavaFlow run " + lavaflowRunID + " isn't waiting for approval", http.StatusConflict,
		},
		"anything else": {
			refusal(http.StatusConflict, "the merge queue is busy", ""),
			"failed to approve LavaFlow run " + lavaflowRunID + "'s merge: HTTP 409: the merge queue is busy", http.StatusConflict,
		},
		"no route": {
			func(s *lavaflowServer) { s.approvalRouteMissing = true },
			"LavaFlow merging isn't available on this Volcano API", http.StatusNotFound,
		},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.waitForApproval(approvalReason("automatic_run", nil))
			tc.refuse(s)
		})

		_, err := executeLavaflowCommand(t, New(deps), "approve", lavaflowRunID, "--yes")
		require.Error(t, err, name)
		assert.Contains(t, err.Error(), tc.want, name)
		assert.Equal(t, tc.status, api.Status(err), name)
	}
}

// refusal refuses every approval with this status, message and code.
func refusal(status int, message, code string) func(*lavaflowServer) {
	return func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = status, http.MethodPost
		s.refusal = map[string]any{"error": message}
		if code != "" {
			s.refusal["code"] = code
		}
	}
}

func TestApproveRefusesARunIDThatIsntOne(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	_, err := executeLavaflowCommand(t, New(deps), "approve", "latest", "--yes")
	require.EqualError(t, err, `invalid run ID "latest": use an ID from 'volcano cloud lavaflow list'`)
	fake.set(func(s *lavaflowServer) { assert.Empty(t, s.queries) })
}
