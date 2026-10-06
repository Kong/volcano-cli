package api

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

const (
	// The third execution's approval timeout. Short enough that the test does
	// not sit on it, long enough that all three approvals are still pending when
	// the list is read.
	apiE2EDurableApprovalExpiry = "1m"
	// The timeout itself, plus the background pass that marks the approval
	// expired, which runs about once a minute.
	apiE2EDurableApprovalExpiryTimeout = 5 * time.Minute
)

// apiE2EDurableApprovalSource is written against the SDK because
// waitForApproval is SDK surface: it opens the approval and registers it with
// the API the function was deployed by. The input chooses the timeout, so one
// function covers approve, deny, and expire.
const apiE2EDurableApprovalSource = `
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

// apiE2EDurableApprovalManifest pins the first SDK release that ships
// ctx.waitForApproval.
const apiE2EDurableApprovalManifest = `{
  "name": "approval-pipeline",
  "private": true,
  "dependencies": {
    "@volcano.dev/sdk": "^1.16.0"
  }
}
`

// TestAPIE2ECloudDurableApprovals walks the approvals group against real
// infrastructure: one workflow, three executions waiting on a person, and each
// of the three ways an approval ends.
//
// The proof that a decision did anything is the execution resuming with it, so
// every decision is followed into the execution's result. The deployed
// function registers its approval with the API under test, which therefore has
// to be reachable from it.
func TestAPIE2ECloudDurableApprovals(t *testing.T) {
	env := setupAPIE2E(t, "cloud-approvals")
	writeAPIE2EDurableApprovalProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "durable", "approvals", "list").requireSuccess(t, "No pending approvals")

	env.runCloudCLI(t, "durable", "deploy", "--all").requireSuccess(t,
		"[1/1] Deploying approval-pipeline", "1/1 durable function(s) deployment started")
	t.Cleanup(func() {
		env.runCloudCLI(t, "durable", "delete", "approval-pipeline", "--yes")
	})
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active",
		"durable", "get", "approval-pipeline")

	approveExecution := startAPIE2EDurableApproval(t, env, "cli-e2e-approve", `{"order":"approve"}`)
	denyExecution := startAPIE2EDurableApproval(t, env, "cli-e2e-deny", `{"order":"deny"}`)
	expireExecution := startAPIE2EDurableApproval(t, env, "cli-e2e-expire",
		`{"order":"expire","timeout":"`+apiE2EDurableApprovalExpiry+`"}`)

	approvals := waitForAPIE2EDurableApprovals(t, env, approveExecution, denyExecution, expireExecution)
	approveID := approvals[approveExecution]
	denyID := approvals[denyExecution]
	expireID := approvals[expireExecution]

	env.runCloudCLI(t, "durable", "approvals", "list", "--function", "approval-pipeline").requireSuccess(t,
		"Showing 3 of 3 approval(s)", "approval-pipeline",
		"Ship order approve?", "Ship order deny?", "Ship order expire?",
		"cli-e2e-approve", "cli-e2e-deny", "cli-e2e-expire")
	env.runCloudCLI(t, "durable", "approvals", "get", approveID).requireSuccess(t,
		"Title: Ship order approve?", "Name: ship-order", "Status: pending",
		"Workflow: approval-pipeline", "cli-e2e-approve", `"order": "approve"`,
		"Expires: when its execution ends")

	env.runCloudCLI(t, "durable", "approvals", "approve", approveID, "--comment", "Checked stock", "--yes").
		requireSuccess(t, "Status: approved", "Comment: Checked stock", `Approved "Ship order approve?"`)
	requireAPIE2EDurableApprovalOutcome(t, env, approveExecution, apiE2EDurableExecutionTimeout,
		`"status": "approved"`, `"approved": true`, `"comment": "Checked stock"`)

	env.runCloudCLI(t, "durable", "approvals", "deny", denyID, "--comment", "Customer cancelled", "--yes").
		requireSuccess(t, "Status: denied", `Denied "Ship order deny?"`)
	requireAPIE2EDurableApprovalOutcome(t, env, denyExecution, apiE2EDurableExecutionTimeout,
		`"status": "denied"`, `"approved": false`, `"comment": "Customer cancelled"`)

	// Nobody decides the third. Its timeout resolves the wait rather than
	// failing the execution, and the approval is marked expired behind it.
	requireAPIE2EDurableApprovalOutcome(t, env, expireExecution, apiE2EDurableApprovalExpiryTimeout,
		`"status": "expired"`, `"approved": false`)
	env.waitForCloudCLIContains(t, apiE2EDurableApprovalExpiryTimeout, "Status: expired",
		"durable", "approvals", "get", expireID)

	env.runCloudCLI(t, "durable", "approvals", "list", "--function", "approval-pipeline").
		requireSuccess(t, "No pending approvals")
	env.runCloudCLI(t, "durable", "approvals", "list", "--function", "approval-pipeline", "--status", "all").
		requireSuccess(t, "Showing 3 of 3 approval(s)", approveID, denyID, expireID)

	env.runCloudCLI(t, "durable", "approvals", "stats", "--since", "1h", "--function", "approval-pipeline").
		requireSuccess(t, "Requested: 3", "Pending: 0", "Approved: 1", "Denied: 1", "Expired: 1",
			"Approval Rate: 50%")

	// The same decision again is a safe retry and changes nothing. A different
	// one, or any decision on an expired approval, says what happened and fails.
	env.runCloudCLI(t, "durable", "approvals", "approve", approveID, "--yes").
		requireSuccess(t, "Already approved by ", "nothing changed")
	env.runCloudCLI(t, "durable", "approvals", "deny", approveID, "--yes").
		requireFailure(t, "approval "+approveID+" was already approved by ")
	env.runCloudCLI(t, "durable", "approvals", "approve", expireID, "--yes").
		requireFailure(t, "approval "+expireID+" expired at ")

	unknown := uuid.NewString()
	env.runCloudCLI(t, "durable", "approvals", "get", unknown).
		requireFailure(t, "no approval "+unknown+" in this project")
}

func startAPIE2EDurableApproval(t *testing.T, env *apiE2E, name, input string) string {
	t.Helper()
	start := env.runCloudCLI(t, "durable", "start", "approval-pipeline", "--input", input, "--name", name)
	start.requireSuccess(t, "Execution "+name+" started")
	return apiE2EDurableExecutionID(t, start.output)
}

// waitForAPIE2EDurableApprovals polls until each execution has asked for its
// approval, and returns approval ids by execution id. An approval appears once
// the execution has run far enough to ask for it, which is after the start
// returns.
func waitForAPIE2EDurableApprovals(t *testing.T, env *apiE2E, executionIDs ...string) map[string]string {
	t.Helper()
	deadline := time.Now().Add(apiE2EDurableExecutionTimeout)
	var last cliResult
	for time.Now().Before(deadline) {
		last = env.runCloudCLIWithin(t, attemptTimeout(deadline),
			"durable", "approvals", "list", "--function", "approval-pipeline", "--status", "all", "--json")
		if last.code == 0 {
			var page apiclient.PaginatedDurableApprovals
			if err := json.Unmarshal([]byte(last.stdout), &page); err != nil {
				t.Fatalf("approvals list --json printed something other than a page: %v\n%s", err, last.output)
			}
			approvals := map[string]string{}
			for _, approval := range page.Data {
				if approval.Execution.Id != nil {
					approvals[approval.Execution.Id.String()] = approval.Id.String()
				}
			}
			if hasEveryKey(approvals, executionIDs) {
				return approvals
			}
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("executions %v never all asked for an approval:\n%s", executionIDs, last.output)
	return nil
}

// requireAPIE2EDurableApprovalOutcome waits for the execution to finish and
// checks the decision ctx.waitForApproval returned into its result.
func requireAPIE2EDurableApprovalOutcome(
	t *testing.T, env *apiE2E, executionID string, timeout time.Duration, needles ...string,
) {
	t.Helper()
	env.waitForCloudCLIContains(t, timeout, "Status: succeeded",
		"durable", "executions", "get", "approval-pipeline", executionID)
	env.runCloudCLI(t, "durable", "executions", "get", "approval-pipeline", executionID).
		requireSuccess(t, needles...)
}

func hasEveryKey(values map[string]string, keys []string) bool {
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

func writeAPIE2EDurableApprovalProject(t *testing.T, projectDir string) {
	t.Helper()
	writeAPIE2EBaseProject(t, projectDir)
	functionDir := filepath.Join(projectDir, "volcano", "functions", "approval-pipeline")
	writeAPIE2EFile(t, filepath.Join(functionDir, "index.js"), apiE2EDurableApprovalSource)
	writeAPIE2EFile(t, filepath.Join(functionDir, "package.json"), apiE2EDurableApprovalManifest)
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano-config.yaml"), `
version: 1
functions:
  - name: hello
  - name: approval-pipeline
    kind: durable
`)
}
