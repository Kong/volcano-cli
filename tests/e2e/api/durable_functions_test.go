package api

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	apiE2EDurableExecutionTimeout = 5 * time.Minute
	apiE2EDurableStopTimeout      = 5 * time.Minute
	// A minute for the next tick, plus room for the run to be claimed and the
	// execution to appear.
	apiE2EDurableSchedulerTickTimeout = 5 * time.Minute
)

// A durable invocation speaks an envelope protocol rather than taking the
// caller's payload as the event, so the fixture is written against the raw
// protocol and depends on nothing but the source it ships. Authoring one with
// the SDK is covered where the SDK is: this suite is about the commands.
const apiE2EDurableSource = `
exports.handler = async (envelope) => ({
  Status: "SUCCEEDED",
  Result: JSON.stringify({ echoed: durableInput(envelope), ran: true }),
});

function durableInput(envelope) {
  const operationId = String(envelope.DurableExecutionArn || "").split("/").pop();
  const state = envelope.InitialExecutionState || {};
  const operations = state.Operations || [];
  const execution = operations.find((op) => op.Id === operationId) || operations[0] || {};
  return (execution.ExecutionDetails || {}).InputPayload || null;
}
`

// apiE2EDurableSuspendedSource never finishes. `PENDING` means "I suspended,
// resume me later", and what resumes an execution is whatever the handler
// checkpointed before returning; this one checkpoints nothing, so it sits in
// `running` until something stops it.
//
// That is the only state `executions stop` can be observed working in: a
// handler that returns `SUCCEEDED` has already finished before a stop lands.
const apiE2EDurableSuspendedSource = `exports.handler = async () => ({ Status: "PENDING" });`

// TestAPIE2ECloudDurableFunctions walks the durable group against real
// infrastructure: declare, deploy, start, poll, page, stop, redeploy, delete.
//
// Every command in the group and every flag on it runs here, in one project,
// because each durable deploy is a real build — two functions cover the whole
// surface where a test per command would pay for a build per command. The
// control-plane refusals (unknown names, bad ids, flag conflicts) are covered
// by the hosting repo's `TestCLIE2ESmokeDurableFunctions`, which needs no
// provisioning.
func TestAPIE2ECloudDurableFunctions(t *testing.T) {
	env := setupAPIE2E(t, "cloud-durable")
	writeAPIE2EDurableProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "durable", "list").requireSuccess(t, "No durable functions")

	// --all takes its targets from the manifest, so both durable entries deploy
	// and the standard one does not.
	env.runCloudCLI(t, "durable", "deploy", "--all").requireSuccess(t,
		"[1/2] Deploying order-pipeline", "[2/2] Deploying sleeper",
		"2/2 durable function(s) deployment started")
	t.Cleanup(func() {
		env.runCloudCLI(t, "durable", "delete", "order-pipeline", "--yes")
		env.runCloudCLI(t, "durable", "delete", "sleeper", "--yes")
	})

	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active",
		"durable", "get", "order-pipeline")
	env.runCloudCLI(t, "durable", "get", "order-pipeline").
		requireSuccess(t, "Anon Key Start: denied", "Execution Timeout:", "Retention:")
	env.runCloudCLI(t, "durable", "list").requireSuccess(t, "order-pipeline", "sleeper", "active")

	// A durable function is in its own collection: the standard commands must
	// not find it, and it must not be deployable as a standard function.
	env.runCloudCLI(t, "functions", "list").requireNotContains(t, "order-pipeline")
	env.runCloudCLI(t, "functions", "get", "order-pipeline").requireFailure(t)
	env.runCloudCLI(t, "functions", "deploy", "-f", "order-pipeline").
		requireFailure(t, "declared durable in volcano-config.yaml")

	requireAPIE2EDurableListPages(t, env)
	requireAPIE2EDurableStarts(t, env)
	requireAPIE2EDurableExecutionPages(t, env)
	requireAPIE2EDurableSchedulers(t, env)
	requireAPIE2EDurableStop(t, env)
	requireAPIE2EDurableVisibility(t, env)

	env.runCloudCLI(t, "durable", "delete", "order-pipeline", "--yes").
		requireSuccess(t, "Durable function 'order-pipeline' deletion started")
	env.runCloudCLI(t, "durable", "delete", "sleeper", "--yes").
		requireSuccess(t, "Durable function 'sleeper' deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No durable functions",
		"durable", "list")
}

// requireAPIE2EDurableListPages covers --page and --limit on the function
// listing. Paging is where a listing command is most often wrong in a way the
// default page hides: a limit that never reaches the API still returns
// everything.
func requireAPIE2EDurableListPages(t *testing.T, env *apiE2E) {
	t.Helper()

	first := env.runCloudCLI(t, "durable", "list", "--limit", "1")
	first.requireSuccess(t, "Showing 1 of 2 durable function(s) (page 1, limit 1)",
		"durable list --page 2 --limit 1")

	second := env.runCloudCLI(t, "durable", "list", "--page", "2", "--limit", "1")
	second.requireSuccess(t, "Showing 1 of 2 durable function(s) (page 2, limit 1)")

	if apiE2EDurableListedName(t, first.output) == apiE2EDurableListedName(t, second.output) {
		t.Fatalf("page 1 and page 2 listed the same durable function:\n%s\n%s",
			first.output, second.output)
	}

	env.runCloudCLI(t, "durable", "list", "--page", "9").
		requireSuccess(t, "No durable functions found on page 9")
}

// requireAPIE2EDurableStarts covers `durable start` in all three input forms —
// inline JSON, a file, and none — and the idempotency key that makes a retried
// start safe.
func requireAPIE2EDurableStarts(t *testing.T, env *apiE2E) {
	t.Helper()

	// Starting is asynchronous by construction: the command returns a handle,
	// and a result exists only once the execution has run.
	start := env.runCloudCLI(t, "durable", "start", "order-pipeline",
		"--input", `{"order_id":4417}`, "--name", "cli-e2e-order")
	start.requireSuccess(t, "Execution cli-e2e-order started")
	executionID := apiE2EDurableExecutionID(t, start.output)

	env.waitForCloudCLIContains(t, apiE2EDurableExecutionTimeout, "Status: succeeded",
		"durable", "executions", "get", "order-pipeline", executionID)
	env.runCloudCLI(t, "durable", "executions", "get", "order-pipeline", executionID).
		requireSuccess(t, "4417", "Duration:")
	env.runCloudCLI(t, "durable", "executions", "list", "order-pipeline", "--status", "succeeded").
		requireSuccess(t, executionID, "cli-e2e-order")

	// The name is the idempotency key, so repeating the start returns the
	// execution that already exists rather than beginning a second.
	env.runCloudCLI(t, "durable", "start", "order-pipeline",
		"--input", `{"order_id":4417}`, "--name", "cli-e2e-order").
		requireSuccess(t, executionID)
	env.runCloudCLI(t, "durable", "executions", "list", "order-pipeline").
		requireSuccess(t, "Showing 1 of 1 execution(s)")

	// --input takes a path as readily as inline JSON, which is how a payload too
	// unwieldy for a shell argument is passed. The file's contents have to reach
	// the function, not merely be accepted: the fixture echoes its input back.
	inputPath := filepath.Join(env.projectDir, "order.json")
	writeAPIE2EFile(t, inputPath, `{"order_id":8231,"source":"file"}`)
	fromFile := env.runCloudCLI(t, "durable", "start", "order-pipeline",
		"--input", inputPath, "--name", "cli-e2e-file")
	fromFile.requireSuccess(t, "Execution cli-e2e-file started")
	fileExecutionID := apiE2EDurableExecutionID(t, fromFile.output)
	env.waitForCloudCLIContains(t, apiE2EDurableExecutionTimeout, "Status: succeeded",
		"durable", "executions", "get", "order-pipeline", fileExecutionID)
	env.runCloudCLI(t, "durable", "executions", "get", "order-pipeline", fileExecutionID).
		requireSuccess(t, "8231", "file")

	// The shortest start there is: no input, and a name Volcano generates. No
	// input reaches the function as no input, which the fixture reports as a
	// null echo rather than an empty object.
	bare := env.runCloudCLI(t, "durable", "start", "order-pipeline")
	bare.requireSuccess(t, "started")
	bareExecutionID := apiE2EDurableExecutionID(t, bare.output)
	env.waitForCloudCLIContains(t, apiE2EDurableExecutionTimeout, "Status: succeeded",
		"durable", "executions", "get", "order-pipeline", bareExecutionID)
	env.runCloudCLI(t, "durable", "executions", "get", "order-pipeline", bareExecutionID).
		requireSuccess(t, `"echoed": null`)
}

// requireAPIE2EDurableExecutionPages covers --page and --limit on the execution
// listing, which by now has three executions to page through.
func requireAPIE2EDurableExecutionPages(t *testing.T, env *apiE2E) {
	t.Helper()

	first := env.runCloudCLI(t, "durable", "executions", "list", "order-pipeline", "--limit", "1")
	first.requireSuccess(t, "Showing 1 of 3 execution(s) (page 1, limit 1)",
		"durable executions list order-pipeline --page 2 --limit 1")

	second := env.runCloudCLI(t, "durable", "executions", "list", "order-pipeline",
		"--page", "2", "--limit", "1")
	second.requireSuccess(t, "Showing 1 of 3 execution(s) (page 2, limit 1)")

	if apiE2EDurableListedExecutionID(t, first.output) ==
		apiE2EDurableListedExecutionID(t, second.output) {
		t.Fatalf("page 1 and page 2 listed the same execution:\n%s\n%s", first.output, second.output)
	}

	// A status none of this function's executions is in filters server-side
	// rather than being dropped on the way.
	env.runCloudCLI(t, "durable", "executions", "list", "order-pipeline", "--status", "stopped").
		requireSuccess(t, `No executions started for durable function "order-pipeline"`)
}

// requireAPIE2EDurableSchedulers covers the schedulers group end to end,
// including the part only real infrastructure can show: a tick starting an
// execution. A scheduler the API accepted but whose ticks never reach the
// durable collection would pass every assertion short of that one.
func requireAPIE2EDurableSchedulers(t *testing.T, env *apiE2E) {
	t.Helper()

	env.runCloudCLI(t, "durable", "schedulers", "list", "order-pipeline").
		requireSuccess(t, `No schedulers configured for durable function "order-pipeline"`)

	// Every minute, so a tick lands inside the wait below.
	created := env.runCloudCLI(t, "durable", "schedulers", "create", "order-pipeline",
		"--name", "cli-e2e-tick", "--cron", "* * * * *", "--input", `{"order_id":9001}`)
	created.requireSuccess(t, `Created scheduler for durable function "order-pipeline"`,
		"cli-e2e-tick", "enabled", "* * * * *")
	schedulerID := apiE2EDurableSchedulerID(t, created.output)
	t.Cleanup(func() {
		env.runCloudCLI(t, "durable", "schedulers", "delete", "order-pipeline", schedulerID, "--yes")
	})

	env.runCloudCLI(t, "durable", "schedulers", "list", "order-pipeline").
		requireSuccess(t, schedulerID, "cli-e2e-tick", "enabled")

	// A tick starts an execution rather than invoking the function, so the proof
	// is an execution in the durable collection named after the scheduler run,
	// not a counter on the scheduler.
	env.waitForCloudCLIContains(t, apiE2EDurableSchedulerTickTimeout, "sched-",
		"durable", "executions", "list", "order-pipeline")

	// Disabling stops the ticks and leaves the scheduler in place. Nothing here
	// asserts that no further tick lands — that would be waiting to observe an
	// absence — only that the state the next tick reads is off.
	env.runCloudCLI(t, "durable", "schedulers", "disable", "order-pipeline", schedulerID).
		requireSuccess(t, "Disabled scheduler "+schedulerID, "disabled")
	env.runCloudCLI(t, "durable", "schedulers", "list", "order-pipeline").
		requireSuccess(t, "disabled")

	env.runCloudCLI(t, "durable", "schedulers", "enable", "order-pipeline", schedulerID).
		requireSuccess(t, "Enabled scheduler "+schedulerID, "enabled")
	env.runCloudCLI(t, "durable", "schedulers", "disable", "order-pipeline", schedulerID).
		requireSuccess(t, "disabled")

	// Deleting the scheduler leaves the executions it started, which are what
	// the rest of this test pages through.
	env.runCloudCLI(t, "durable", "schedulers", "delete", "order-pipeline", schedulerID, "--yes").
		requireSuccess(t, "Deleted scheduler "+schedulerID)
	env.runCloudCLI(t, "durable", "schedulers", "list", "order-pipeline").
		requireSuccess(t, `No schedulers configured for durable function "order-pipeline"`)
	env.runCloudCLI(t, "durable", "schedulers", "delete", "order-pipeline", schedulerID, "--yes").
		requireFailure(t, schedulerID)
}

// requireAPIE2EDurableStop covers the one command that changes a running
// execution. Stop answers with the execution rather than a status of its own,
// so a stop that reported success while the execution kept running would look
// identical at the call site — the assertion has to be the state afterwards.
func requireAPIE2EDurableStop(t *testing.T, env *apiE2E) {
	t.Helper()

	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active",
		"durable", "get", "sleeper")

	start := env.runCloudCLI(t, "durable", "start", "sleeper", "--name", "cli-e2e-stop")
	start.requireSuccess(t, "Execution cli-e2e-stop started")
	executionID := apiE2EDurableExecutionID(t, start.output)

	// A suspended durable execution reports `running` while no invocation of it
	// is live, which is the state stopping one is meaningful in.
	env.waitForCloudCLIContains(t, apiE2EDurableStopTimeout, "Status: running",
		"durable", "executions", "get", "sleeper", executionID)

	env.runCloudCLI(t, "durable", "executions", "stop", "sleeper", executionID, "--yes").
		requireSuccess(t, "Stop requested for execution "+executionID)
	env.waitForCloudCLIContains(t, apiE2EDurableStopTimeout, "Status: stopped",
		"durable", "executions", "get", "sleeper", executionID)

	// Stopping a terminal execution reports where it is rather than failing, so
	// a retried stop is safe.
	env.runCloudCLI(t, "durable", "executions", "stop", "sleeper", executionID, "--yes").
		requireSuccess(t, "Status: stopped")
	env.runCloudCLI(t, "durable", "executions", "list", "sleeper", "--status", "stopped").
		requireSuccess(t, executionID)
}

// requireAPIE2EDurableVisibility covers `deploy -f` and the two visibility
// flags. Visibility travels with a deploy because a durable function has no
// update endpoint, so getting this wrong means a function that cannot be made
// public at all — or worse, one that quietly becomes public on a redeploy.
func requireAPIE2EDurableVisibility(t *testing.T, env *apiE2E) {
	t.Helper()

	// -f deploys by name whether or not the manifest mentions the function, and
	// visibility is applied when the deploy is accepted rather than when the
	// build finishes.
	env.runCloudCLI(t, "durable", "deploy", "-f", "order-pipeline", "--public").
		requireSuccess(t, "Deploying order-pipeline", "1/1 durable function(s) deployment started")
	env.runCloudCLI(t, "durable", "get", "order-pipeline").
		requireSuccess(t, "Anon Key Start: allowed")

	// A redeploy has to be able to take it back. Each of these is a real build,
	// so waiting for the rollout in between is the price of not racing it.
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active",
		"durable", "get", "order-pipeline")
	env.runCloudCLI(t, "durable", "deploy", "-f",
		filepath.Join("volcano", "functions", "order-pipeline.js"), "--private").
		requireSuccess(t, "1/1 durable function(s) deployment started")
	env.runCloudCLI(t, "durable", "get", "order-pipeline").
		requireSuccess(t, "Anon Key Start: denied")

	// Leave no build in flight, or the delete this test ends with races the
	// rollout.
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active",
		"durable", "get", "order-pipeline")
}

// apiE2EDurableExecutionID reads the id out of what `durable start` printed,
// which is the only place the caller learns it.
func apiE2EDurableExecutionID(t *testing.T, output string) string {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		if id, found := strings.CutPrefix(strings.TrimSpace(line), "ID:"); found {
			return strings.TrimSpace(id)
		}
	}
	t.Fatalf("durable start printed no execution ID:\n%s", output)
	return ""
}

// apiE2EDurableSchedulerID reads the id out of what `schedulers create`
// printed. Every later scheduler command takes it, and it is the only place the
// caller learns it.
func apiE2EDurableSchedulerID(t *testing.T, output string) string {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		id, found := strings.CutPrefix(strings.TrimSpace(line), "ID:")
		if !found {
			continue
		}
		if _, err := uuid.Parse(strings.TrimSpace(id)); err == nil {
			return strings.TrimSpace(id)
		}
	}
	t.Fatalf("durable schedulers create printed no scheduler ID:\n%s", output)
	return ""
}

// apiE2EDurableListedName reads the one function name out of a single-row
// listing, which is how the paging assertions tell two pages apart.
func apiE2EDurableListedName(t *testing.T, output string) string {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		name, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if name == "order-pipeline" || name == "sleeper" {
			return name
		}
	}
	t.Fatalf("no durable function row in listing:\n%s", output)
	return ""
}

// apiE2EDurableListedExecutionID reads the one execution id out of a single-row
// listing. The id is the first column, and it is the only uuid-shaped field a
// row starts with.
func apiE2EDurableListedExecutionID(t *testing.T, output string) string {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		field, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		if _, err := uuid.Parse(field); err == nil {
			return field
		}
	}
	t.Fatalf("no execution row in listing:\n%s", output)
	return ""
}

func writeAPIE2EDurableProject(t *testing.T, projectDir string) {
	t.Helper()
	writeAPIE2EBaseProject(t, projectDir)
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano", "functions", "order-pipeline.js"),
		apiE2EDurableSource)
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano", "functions", "sleeper.js"),
		apiE2EDurableSuspendedSource)
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano-config.yaml"), `
version: 1
functions:
  - name: hello
  - name: order-pipeline
    kind: durable
  - name: sleeper
    kind: durable
`)
}
