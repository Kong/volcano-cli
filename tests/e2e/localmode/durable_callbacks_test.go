package localmode

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// localModeE2EDurableCallbackSource is the cloud E2E's workflow: callbacks work
// the same in both modes, and so does the code a user writes for them. Each
// submitter logs its token for the test to answer with.
const localModeE2EDurableCallbackSource = `
const { durable, CallbackFailedError } = require('@volcano.dev/sdk/durable');

const handOff = (name) => (token) => {
  console.log('callback-token ' + name + ' ' + token);
};

exports.handler = durable(async (input, ctx) => {
  const paid = await ctx.waitForCallback('payment', handOff('payment'), {
    timeout: '10m',
    heartbeatTimeout: '5m',
  });
  let declined = null;
  try {
    await ctx.waitForCallback('refund', handOff('refund'), { timeout: '10m' });
  } catch (error) {
    if (!(error instanceof CallbackFailedError)) throw error;
    declined = { type: error.type, message: error.message };
  }
  return { paid, declined };
});
`

// localModeE2EDurableCallbackManifest pins the first SDK release that ships
// ctx.waitForCallback.
const localModeE2EDurableCallbackManifest = `{
  "name": "callback-pipeline",
  "private": true,
  "dependencies": {
    "@volcano.dev/sdk": "^2.2.0"
  }
}
`

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
	writeLocalModeE2EFile(t, dir, filepath.Join(functionDir, "index.js"), localModeE2EDurableCallbackSource)
	writeLocalModeE2EFile(t, dir, filepath.Join(functionDir, "package.json"), localModeE2EDurableCallbackManifest)
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
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "callbacks", "succeed", payment, "--result", `{"charge_id":"ch_4417"}`),
		"Succeeded the callback; the execution resumes with the result")
	answered, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
		"durable", "callbacks", "succeed", payment, "--result", "1")
	require.Error(t, err, "a token takes one answer\n%s", answered)
	requireContains(t, answered, "the callback already has an outcome")

	refund := waitForLocalModeE2ECallbackToken(t, binary, env, dir, "refund")
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "callbacks", "fail", refund, "--type", "CardDeclined", "--message", "Insufficient funds", "--json"),
		`"status": "failed"`)

	execution := awaitLocalModeE2EDurableExecution(t, binary, env, dir, "callback-pipeline", executionID)
	requireContains(t, execution, "Status: succeeded")
	requireContains(t, execution, `"charge_id": "ch_4417"`)
	requireContains(t, execution, `"type": "CardDeclined"`)

	unknown, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
		"durable", "callbacks", "heartbeat", "vcb_0000000000000000000000000000000000000000000")
	require.Error(t, err, "a token nobody issued has to be refused\n%s", unknown)
	requireContains(t, unknown, "no callback has this token")
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
