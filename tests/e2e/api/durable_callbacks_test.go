package api

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// apiE2EDurableCallbackSource waits on two callbacks: payment is succeeded and
// refund is failed. Each submitter logs its token, which the test reads back
// from the runtime logs to play the system the token was handed to. A real
// workflow sends it somewhere instead: a token is a secret.
const apiE2EDurableCallbackSource = `
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

// apiE2EDurableCallbackManifest pins the first SDK release that ships
// ctx.waitForCallback.
const apiE2EDurableCallbackManifest = `{
  "name": "callback-pipeline",
  "private": true,
  "dependencies": {
    "@volcano.dev/sdk": "^2.2.0"
  }
}
`

// apiE2EUnknownCallbackToken is well formed and was never issued.
const apiE2EUnknownCallbackToken = "vcb_0000000000000000000000000000000000000000000"

// TestAPIE2ECloudDurableCallbacks walks the callbacks group against real
// infrastructure: a heartbeat and a success on one token, a failure on the
// next, each followed into the execution that resumes with it.
//
// The answers run without a login, the way the system a workflow hands a token
// to would run them.
func TestAPIE2ECloudDurableCallbacks(t *testing.T) {
	env := setupAPIE2E(t, "cloud-callbacks")
	writeAPIE2EDurableCallbackProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "durable", "deploy", "--all").requireSuccess(t,
		"[1/1] Deploying callback-pipeline", "1/1 durable function(s) deployment started")
	t.Cleanup(func() {
		env.runCloudCLI(t, "durable", "delete", "callback-pipeline", "--yes")
	})
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active",
		"durable", "get", "callback-pipeline")

	start := env.runCloudCLI(t, "durable", "start", "callback-pipeline", "--name", "cli-e2e-callbacks")
	start.requireSuccess(t, "Execution cli-e2e-callbacks started")
	executionID := apiE2EDurableExecutionID(t, start.output)

	holder := []string{"HOME=" + t.TempDir()}
	payment := waitForAPIE2ECallbackToken(t, env, "payment")
	env.runCloudCLIWithEnv(t, holder, "durable", "callbacks", "heartbeat", payment).
		requireSuccess(t, "Heartbeat sent; the callback is still waiting")
	requireAPIE2ECallbackAccepted(t, env.runCloudCLIWithEnv(t, holder, "durable", "callbacks", "succeed", payment,
		"--result", `{"charge_id":"ch_4417"}`, "--json"), "succeeded")
	env.runCloudCLIWithEnv(t, holder, "durable", "callbacks", "succeed", payment, "--result", "1").
		requireFailure(t, "the callback already has an outcome")
	env.runCloudCLIWithEnv(t, holder, "durable", "callbacks", "heartbeat", payment).
		requireFailure(t, "the callback already has an outcome")

	refund := waitForAPIE2ECallbackToken(t, env, "refund")
	requireAPIE2ECallbackAccepted(t, env.runCloudCLIWithEnv(t, holder, "durable", "callbacks", "fail", refund,
		"--type", "CardDeclined", "--message", "Insufficient funds", "--json"), "failed")

	env.waitForCloudCLIContains(t, apiE2EDurableExecutionTimeout, "Status: succeeded",
		"durable", "executions", "get", "callback-pipeline", executionID)
	env.runCloudCLI(t, "durable", "executions", "get", "callback-pipeline", executionID).
		requireSuccess(t, `"charge_id": "ch_4417"`, `"type": "CardDeclined"`, `"message": "Insufficient funds"`)

	env.runCloudCLIWithEnv(t, holder, "durable", "callbacks", "heartbeat", apiE2EUnknownCallbackToken).
		requireFailure(t, "no callback has this token")
	env.runCloudCLIWithEnv(t, holder, "durable", "callbacks", "heartbeat", "vcb_not/a/token").
		requireFailure(t, "invalid callback token")
}

// requireAPIE2ECallbackAccepted passes an answer the execution has, or one
// Volcano recorded and is still delivering. The execution's own status is what
// shows the answer arrived.
func requireAPIE2ECallbackAccepted(t *testing.T, answer cliResult, status string) {
	t.Helper()
	answer.requireSuccess(t)
	if !strings.Contains(answer.output, `"status": "`+status+`"`) &&
		!strings.Contains(answer.output, `"status": "delivering"`) {
		t.Fatalf("the answer was neither %s nor delivering:\n%s", status, redactCredentials(answer.output))
	}
}

// waitForAPIE2ECallbackToken polls the runtime logs until the submitter of the
// named callback has logged its token. The submitter runs once the wait opens,
// after the start returns, and the log takes a while to be searchable.
func waitForAPIE2ECallbackToken(t *testing.T, env *apiE2E, name string) string {
	t.Helper()
	logged := regexp.MustCompile(`callback-token ` + regexp.QuoteMeta(name) + ` (vcb_[A-Za-z0-9_-]+)`)
	deadline := time.Now().Add(apiE2EDurableExecutionTimeout)
	var last cliResult
	for time.Now().Before(deadline) {
		last = env.runCloudCLIWithin(t, attemptTimeout(deadline),
			"durable", "logs", "callback-pipeline", "--type", "runtime")
		if match := logged.FindStringSubmatch(last.output); last.code == 0 && match != nil {
			return match[1]
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("the %s callback never logged its token:\n%s", name, redactCredentials(last.output))
	return ""
}

func writeAPIE2EDurableCallbackProject(t *testing.T, projectDir string) {
	t.Helper()
	writeAPIE2EBaseProject(t, projectDir)
	functionDir := filepath.Join(projectDir, "volcano", "functions", "callback-pipeline")
	writeAPIE2EFile(t, filepath.Join(functionDir, "index.js"), apiE2EDurableCallbackSource)
	writeAPIE2EFile(t, filepath.Join(functionDir, "package.json"), apiE2EDurableCallbackManifest)
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano-config.yaml"), `
version: 1
functions:
  - name: hello
  - name: callback-pipeline
    kind: durable
`)
}
