package api

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// apiE2EDurableMessageSource waits for a `posted` message and publishes around
// the wait, so one execution has messages in both directions. The SDK opens
// the wait and the publish session with the API the function was deployed by.
const apiE2EDurableMessageSource = `
const { durable } = require('@volcano.dev/sdk/durable');

exports.handler = durable(async (input, ctx) => {
  await ctx.publish('progress', { stage: 'waiting' });
  const posted = await ctx.waitForMessage('posted', { timeout: '10m' });
  if (!posted) {
    return { url: null };
  }
  await ctx.publish('progress', { stage: 'posted' });
  return { url: posted.data.url, sender: posted.sender.type };
});
`

// apiE2EDurableMessageManifest pins the first SDK release that ships
// ctx.waitForMessage and ctx.publish.
const apiE2EDurableMessageManifest = `{
  "name": "message-pipeline",
  "private": true,
  "dependencies": {
    "@volcano.dev/sdk": "^2.1.0"
  }
}
`

// TestAPIE2ECloudDurableMessages walks the messages group against real
// infrastructure: a workflow that waits for a message and publishes around the
// wait, sent to and read from the CLI.
//
// The proof that a send did anything is the execution resuming with its data,
// so the send is followed into the execution's result.
func TestAPIE2ECloudDurableMessages(t *testing.T) {
	env := setupAPIE2E(t, "cloud-messages")
	writeAPIE2EDurableMessageProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "durable", "deploy", "--all").requireSuccess(t,
		"[1/1] Deploying message-pipeline", "1/1 durable function(s) deployment started")
	t.Cleanup(func() {
		env.runCloudCLI(t, "durable", "delete", "message-pipeline", "--yes")
	})
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active",
		"durable", "get", "message-pipeline")

	start := env.runCloudCLI(t, "durable", "start", "message-pipeline", "--name", "cli-e2e-messages")
	start.requireSuccess(t, "Execution cli-e2e-messages started")
	executionID := apiE2EDurableExecutionID(t, start.output)

	waitForAPIE2EDurableMessageWait(t, env, executionID, "posted")
	env.runCloudCLI(t, "durable", "messages", "list", "message-pipeline", executionID, "--direction", "out").
		requireSuccess(t, "progress", `{"stage":"waiting"}`, "published", "Showing 1 message(s)")
	requireAPIE2EDurableMessagesNeedAWritableToken(t, env, executionID)

	data := `{"url":"https://example.com/posts/4417"}`
	env.runCloudCLI(t, "durable", "messages", "send", "message-pipeline", executionID, "posted",
		"--data", data, "--idempotency-key", "post-4417").
		requireSuccess(t, "Name: posted", "Sender: project", `"url": "https://example.com/posts/4417"`,
			`Sent "posted" to execution `+executionID)
	env.runCloudCLI(t, "durable", "messages", "send", "message-pipeline", executionID, "posted",
		"--data", data, "--idempotency-key", "post-4417").
		requireSuccess(t, "Already sent with this idempotency key; nothing changed")

	env.waitForCloudCLIContains(t, apiE2EDurableExecutionTimeout, "Status: succeeded",
		"durable", "executions", "get", "message-pipeline", executionID)
	env.runCloudCLI(t, "durable", "executions", "get", "message-pipeline", executionID).
		requireSuccess(t, `"url": "https://example.com/posts/4417"`, `"sender": "project"`)

	// The execution has ended, so a follow reads what it published and stops.
	followed := env.runCloudCLI(t, "durable", "messages", "list", "message-pipeline", executionID,
		"--direction", "out", "--follow", "--json")
	followed.requireSuccess(t)
	requireAPIE2EDurableMessageStages(t, followed.stdout, "waiting", "posted")

	env.runCloudCLI(t, "durable", "messages", "list", "message-pipeline", executionID).
		requireSuccess(t, "Showing 3 message(s)", "delivered", "posted")
	env.runCloudCLI(t, "durable", "messages", "waits", "message-pipeline", executionID).
		requireSuccess(t, "Execution "+executionID+" is not waiting for a message")
	env.runCloudCLI(t, "durable", "messages", "send", "message-pipeline", executionID, "posted").
		requireFailure(t, "execution "+executionID+" has ended and takes no more messages")
}

// requireAPIE2EDurableMessagesNeedAWritableToken checks that a read-only
// project access token can read an execution's messages but not send one. The
// execution is still waiting, so the refusal can only be about the token.
func requireAPIE2EDurableMessagesNeedAWritableToken(t *testing.T, env *apiE2E, executionID string) {
	t.Helper()
	name := "cli-e2e-messages-" + apiE2ESuffix(t)
	created := env.runCloudCLI(t, "access-tokens", "create", name, "--scope", "read_only", "--json")
	_, secret := apiE2ECreatedAccessToken(t, created)
	t.Cleanup(func() {
		if revoked := env.runCloudCLI(t, "access-tokens", "revoke", name, "--yes"); revoked.code != 0 {
			t.Errorf("failed to revoke the access token this test minted:\n%s", redactCredentials(revoked.output))
		}
	})

	projectTokenEnv := []string{"VOLCANO_TOKEN=" + secret, "VOLCANO_PROJECT_ID=" + env.projectID}
	env.runCloudCLIWithEnv(t, projectTokenEnv, "durable", "messages", "list", "message-pipeline", executionID).
		requireSuccess(t, "progress")
	env.runCloudCLIWithEnv(t, projectTokenEnv, "durable", "messages", "send", "message-pipeline", executionID, "posted").
		requireFailure(t, "HTTP 403: project access token is read-only")
}

// waitForAPIE2EDurableMessageWait polls until the execution waits for a
// message with the name. The wait opens once the execution has run that far,
// which is after the start returns.
func waitForAPIE2EDurableMessageWait(t *testing.T, env *apiE2E, executionID, name string) {
	t.Helper()
	deadline := time.Now().Add(apiE2EDurableExecutionTimeout)
	var last cliResult
	for time.Now().Before(deadline) {
		last = env.runCloudCLIWithin(t, attemptTimeout(deadline),
			"durable", "messages", "waits", "message-pipeline", executionID, "--json")
		if last.code == 0 {
			var waits apiclient.DurableMessageWaitList
			if err := json.Unmarshal([]byte(last.stdout), &waits); err != nil {
				t.Fatalf("messages waits --json printed something other than a wait list: %v\n%s", err, last.output)
			}
			for _, wait := range waits.Data {
				if wait.Name == name && wait.Status == apiclient.DurableMessageWaitStatusWaiting {
					return
				}
			}
		}
		time.Sleep(apiE2EPollInterval)
	}
	t.Fatalf("execution %s never waited for a %q message:\n%s", executionID, name, last.output)
}

// requireAPIE2EDurableMessageStages checks a --follow --json read: one
// published message per line, carrying each stage in order.
func requireAPIE2EDurableMessageStages(t *testing.T, stdout string, stages ...string) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != len(stages) {
		t.Fatalf("followed %d message(s), want %d:\n%s", len(lines), len(stages), stdout)
	}
	for i, line := range lines {
		var message apiclient.DurableMessage
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			t.Fatalf("line %d is not a message: %v\n%s", i+1, err, line)
		}
		var data struct {
			Stage string `json:"stage"`
		}
		if err := json.Unmarshal(message.Data, &data); err != nil || data.Stage != stages[i] {
			t.Fatalf("message %d carried %s, want stage %q", i+1, message.Data, stages[i])
		}
	}
}

func writeAPIE2EDurableMessageProject(t *testing.T, projectDir string) {
	t.Helper()
	writeAPIE2EBaseProject(t, projectDir)
	functionDir := filepath.Join(projectDir, "volcano", "functions", "message-pipeline")
	writeAPIE2EFile(t, filepath.Join(functionDir, "index.js"), apiE2EDurableMessageSource)
	writeAPIE2EFile(t, filepath.Join(functionDir, "package.json"), apiE2EDurableMessageManifest)
	writeAPIE2EFile(t, filepath.Join(projectDir, "volcano-config.yaml"), `
version: 1
functions:
  - name: hello
  - name: message-pipeline
    kind: durable
`)
}
