package localmode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// localModeE2EDurableApprovalSource is the cloud E2E's workflow: the approval
// flow is the same in both modes, and so is the code a user writes for it.
const localModeE2EDurableApprovalSource = `
const { durable } = require('@volcano.dev/sdk/durable');

exports.handler = durable(async (input, ctx) => {
  const options = {
    title: 'Ship order ' + input.order + '?',
    description: 'Stock is reserved for the CLI E2E.',
    details: { order: input.order },
  };
  if (input.timeout) {
    options.timeout = input.timeout;
  }
  const decision = await ctx.waitForApproval('ship-order', options);
  return { order: input.order, decision };
});
`

// localModeE2EDurableApprovalManifest pins the first SDK release that ships
// ctx.waitForApproval.
const localModeE2EDurableApprovalManifest = `{
  "name": "approval-pipeline",
  "private": true,
  "dependencies": {
    "@volcano.dev/sdk": "^1.16.0"
  }
}
`

// Unlike a wait, an approval timeout is not cut short locally, so the expiry
// takes as long as it says.
const localModeE2EDurableApprovalExpiry = "30s"

// requireLocalModeRunsDurableApprovals is the cloud approvals round trip
// against the local stack: each of the three ways an approval ends, followed
// into the execution that resumes with it. The local session decides as the
// local user, so no login is involved.
func requireLocalModeRunsDurableApprovals(t *testing.T, binary string, env []string, dir string) {
	t.Helper()

	functionDir := filepath.Join("volcano", "functions", "approval-pipeline")
	writeLocalModeE2EFile(t, dir, "volcano-config.yaml", `
version: 1
functions:
  - name: hello
  - name: approval-pipeline
    kind: durable
`)
	writeLocalModeE2EFile(t, dir, filepath.Join(functionDir, "index.js"), localModeE2EDurableApprovalSource)
	writeLocalModeE2EFile(t, dir, filepath.Join(functionDir, "package.json"), localModeE2EDurableApprovalManifest)

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "approvals", "list"),
		"No pending approvals")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "deploy", "--all"),
		"approval-pipeline")
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, "Status: active",
		"durable", "get", "approval-pipeline")

	approveExecution := startLocalModeE2EDurableApproval(t, binary, env, dir, "cli-local-approve", `{"order":"approve"}`)
	denyExecution := startLocalModeE2EDurableApproval(t, binary, env, dir, "cli-local-deny", `{"order":"deny"}`)
	expireExecution := startLocalModeE2EDurableApproval(t, binary, env, dir, "cli-local-expire",
		`{"order":"expire","timeout":"`+localModeE2EDurableApprovalExpiry+`"}`)

	approvals := waitForLocalModeE2EDurableApprovals(t, binary, env, dir,
		approveExecution, denyExecution, expireExecution)
	approveID := approvals[approveExecution]
	denyID := approvals[denyExecution]
	expireID := approvals[expireExecution]

	get := runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "approvals", "get", approveID)
	for _, needle := range []string{
		"Title: Ship order approve?", "Status: pending", "Workflow: approval-pipeline",
		"cli-local-approve", `"order": "approve"`,
	} {
		requireContains(t, get, needle)
	}

	approved := runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "approvals", "approve", approveID, "--comment", "Checked stock", "--yes")
	requireContains(t, approved, "Status: approved")
	requireContains(t, approved, "Comment: Checked stock")
	requireLocalModeE2EDurableApprovalOutcome(t, binary, env, dir, approveExecution,
		`"status": "approved"`, `"approved": true`, `"comment": "Checked stock"`)

	denied := runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "approvals", "deny", denyID, "--comment", "Customer cancelled", "--yes")
	requireContains(t, denied, "Status: denied")
	requireLocalModeE2EDurableApprovalOutcome(t, binary, env, dir, denyExecution,
		`"status": "denied"`, `"approved": false`, `"comment": "Customer cancelled"`)

	requireLocalModeE2EDurableApprovalOutcome(t, binary, env, dir, expireExecution,
		`"status": "expired"`, `"approved": false`)
	waitForLocalModeE2EApprovalStatus(t, binary, env, dir, expireID, "expired")

	stats := runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "approvals", "stats", "--since", "1h", "--function", "approval-pipeline")
	for _, needle := range []string{
		"Requested: 3", "Pending: 0", "Approved: 1", "Denied: 1", "Expired: 1", "Approval Rate: 50%",
	} {
		requireContains(t, stats, needle)
	}

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "approvals", "approve", approveID, "--yes"), "nothing changed")
	conflict, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
		"durable", "approvals", "deny", approveID, "--yes")
	require.Error(t, err, "denying an approved approval has to fail\n%s", conflict)
	requireContains(t, conflict, "approval "+approveID+" was already approved by ")

	unknown := uuid.NewString()
	missing, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "durable", "approvals", "get", unknown)
	require.Error(t, err, "an unknown approval has to fail\n%s", missing)
	requireContains(t, missing, "no approval "+unknown+" in this project")

	runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "delete", "approval-pipeline", "--yes")

	// Removed here for the reason requireLocalModeRunsDurableFunctions gives.
	removeLocalModeE2EFile(t, dir, "volcano-config.yaml")
	require.NoError(t, os.RemoveAll(filepath.Join(dir, functionDir)))
}

func startLocalModeE2EDurableApproval(t *testing.T, binary string, env []string, dir, name, input string) string {
	t.Helper()
	output := runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "start", "approval-pipeline", "--input", input, "--name", name)
	return localModeE2EFieldValue(t, output, "ID")
}

// waitForLocalModeE2EDurableApprovals polls until each execution has asked for
// its approval, and returns approval ids by execution id.
func waitForLocalModeE2EDurableApprovals(
	t *testing.T, binary string, env []string, dir string, executionIDs ...string,
) map[string]string {
	t.Helper()

	deadline := time.Now().Add(3 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		var err error
		last, err = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
			"durable", "approvals", "list", "--function", "approval-pipeline", "--status", "all", "--json")
		if err == nil {
			var page apiclient.PaginatedDurableApprovals
			require.NoError(t, json.Unmarshal([]byte(last), &page), last)
			approvals := map[string]string{}
			for _, approval := range page.Data {
				if approval.Execution.Id != nil {
					approvals[approval.Execution.Id.String()] = approval.Id.String()
				}
			}
			if len(approvals) == len(executionIDs) {
				for _, id := range executionIDs {
					require.Contains(t, approvals, id, "an approval belongs to an execution this test did not start")
				}
				return approvals
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("executions %v never all asked for an approval:\n%s", executionIDs, last)
	return nil
}

// requireLocalModeE2EDurableApprovalOutcome checks the decision
// ctx.waitForApproval returned into the finished execution's result.
func requireLocalModeE2EDurableApprovalOutcome(
	t *testing.T, binary string, env []string, dir, executionID string, needles ...string,
) {
	t.Helper()
	execution := awaitLocalModeE2EDurableExecution(t, binary, env, dir, "approval-pipeline", executionID)
	requireContains(t, execution, "Status: succeeded")
	for _, needle := range needles {
		requireContains(t, execution, needle)
	}
}

// waitForLocalModeE2EApprovalStatus waits out the background pass that marks
// an approval expired, which can trail the execution resuming.
func waitForLocalModeE2EApprovalStatus(t *testing.T, binary string, env []string, dir, approvalID, status string) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		last = runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "approvals", "get", approvalID)
		if strings.Contains(last, "Status: "+status) {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("approval %s never became %s:\n%s", approvalID, status, last)
}
