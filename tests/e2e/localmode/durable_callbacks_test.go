package localmode

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// localModeE2EDurableCallbackSource is the cloud E2E's workflow in Python:
// callbacks work the same in both modes, and so does the code a user writes for
// them. Each submitter prints its token for the test to answer with.
const localModeE2EDurableCallbackSource = `
from volcano_sdk.durable_authoring import CallbackFailedError, durable


def hand_off(name):
    def submit(token):
        print(f"callback-token {name} {token}", flush=True)

    return submit


@durable
def handler(event, ctx):
    paid = ctx.wait_for_callback("payment", hand_off("payment"), timeout="10m", heartbeat_timeout="5m")
    declined = None
    try:
        ctx.wait_for_callback("refund", hand_off("refund"), timeout="10m")
    except CallbackFailedError as error:
        declined = {"type": error.type, "message": error.message}
    return {"paid": paid, "declined": declined}
`

// localModeE2EDurableCallbackRequirements pins the first volcano-sdk-python
// release with ctx.wait_for_callback.
const localModeE2EDurableCallbackRequirements = "volcano-sdk-python~=0.16.0\n"

// TestLocalModeE2EDurableCallbacks is the cloud callbacks round trip against
// the local stack: a heartbeat and a success on one token, a failure on the
// next, and the execution resuming with both. It has its own stack and
// project, so a failure here does not stop the smoke's later checks.
func TestLocalModeE2EDurableCallbacks(t *testing.T) {
	if os.Getenv("VOLCANO_LOCALMODE_E2E") != "1" {
		t.Skip("set VOLCANO_LOCALMODE_E2E=1 to run disposable Docker acceptance")
	}
	requireDocker(t)
	binary := buildLocalModeE2EBinary(t)
	env, dir := localModeE2EEnv(t), localModeE2EProjectDir(t)
	cleanup := func() {
		_, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "stop", "--clean")
	}
	cleanup()
	t.Cleanup(cleanup)

	functionDir := filepath.Join("volcano", "functions", "callback-pipeline")
	writeLocalModeE2EFile(t, dir, "volcano-config.yaml", `
version: 1
functions:
  - name: callback-pipeline
    kind: durable
`)
	writeLocalModeE2EFile(t, dir, filepath.Join(functionDir, "main.py"), localModeE2EDurableCallbackSource)
	writeLocalModeE2EFile(t, dir, filepath.Join(functionDir, "requirements.txt"), localModeE2EDurableCallbackRequirements)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "start"),
		"Volcano is ready for local development.")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "deploy", "--all"),
		"callback-pipeline")
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, "Status: active",
		"durable", "get", "callback-pipeline")

	started := runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "start", "callback-pipeline", "--name", "cli-local-callbacks")
	executionID := localModeE2EFieldValue(t, started, "ID")

	payment := waitForLocalModeE2ECallbackToken(t, binary, env, dir, "payment")
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "callbacks", "heartbeat", payment), "Heartbeat sent; the callback is still waiting")
	requireLocalModeE2ECallbackAccepted(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "callbacks", "succeed", payment, "--result", `{"charge_id":"ch_4417"}`, "--json"), "succeeded")
	answered, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
		"durable", "callbacks", "succeed", payment, "--result", "1")
	require.Error(t, err, "a token takes one answer\n%s", answered)
	requireContains(t, answered, "the callback already has an outcome")

	refund := waitForLocalModeE2ECallbackToken(t, binary, env, dir, "refund")
	requireLocalModeE2ECallbackAccepted(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "callbacks", "fail", refund, "--type", "CardDeclined", "--message", "Insufficient funds", "--json"),
		"failed")

	execution := awaitLocalModeE2EDurableExecution(t, binary, env, dir, "callback-pipeline", executionID)
	requireContains(t, execution, "Status: succeeded")
	requireContains(t, execution, `"charge_id": "ch_4417"`)
	requireContains(t, execution, `"type": "CardDeclined"`)
	requireLocalModeSearchesProjectLogs(t, binary, env, dir)

	unknown, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
		"durable", "callbacks", "heartbeat", "vcb_0000000000000000000000000000000000000000000")
	require.Error(t, err, "a token nobody issued has to be refused\n%s", unknown)
	requireContains(t, unknown, "no callback has this token")
}

// requireLocalModeE2ECallbackAccepted passes an answer the execution has, or
// one Volcano recorded and is still delivering. The execution's own status is
// what shows the answer arrived.
func requireLocalModeE2ECallbackAccepted(t *testing.T, answer, status string) {
	t.Helper()
	if !strings.Contains(answer, `"status": "`+status+`"`) && !strings.Contains(answer, `"status": "delivering"`) {
		t.Fatalf("the answer was neither %s nor delivering:\n%s", status, answer)
	}
}

// waitForLocalModeE2ECallbackToken polls the runtime logs until the submitter
// of the named callback has logged its token.
func waitForLocalModeE2ECallbackToken(t *testing.T, binary string, env []string, dir, name string) string {
	t.Helper()

	logged := regexp.MustCompile(`callback-token ` + regexp.QuoteMeta(name) + ` (vcb_[A-Za-z0-9_-]+)`)
	deadline := time.Now().Add(3 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		var err error
		last, err = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
			"durable", "logs", "callback-pipeline", "--type", "runtime")
		if match := logged.FindStringSubmatch(last); err == nil && match != nil {
			return match[1]
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("the %s callback never logged its token:\n%s", name, last)
	return ""
}
