package localmode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// localModeE2EDurableMessageSource is the cloud E2E's workflow: messages work
// the same in both modes, and so does the code a user writes for them.
const localModeE2EDurableMessageSource = `
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

// localModeE2EDurableMessageManifest pins the first SDK release that ships
// ctx.waitForMessage and ctx.publish.
const localModeE2EDurableMessageManifest = `{
  "name": "message-pipeline",
  "private": true,
  "dependencies": {
    "@volcano.dev/sdk": "^2.1.0"
  }
}
`

// TestLocalModeE2EDurableMessages is the cloud messages round trip against the
// local stack: a wait the CLI sees open, a send it resumes with, and the
// messages the execution published around it. It has its own stack and
// project, so a failure here does not stop the smoke's later checks.
func TestLocalModeE2EDurableMessages(t *testing.T) {
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

	functionDir := filepath.Join("volcano", "functions", "message-pipeline")
	writeLocalModeE2EFile(t, dir, "volcano-config.yaml", `
version: 1
functions:
  - name: message-pipeline
    kind: durable
`)
	writeLocalModeE2EFile(t, dir, filepath.Join(functionDir, "index.js"), localModeE2EDurableMessageSource)
	writeLocalModeE2EFile(t, dir, filepath.Join(functionDir, "package.json"), localModeE2EDurableMessageManifest)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "start"),
		"Volcano is ready for local development.")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "deploy", "--all"),
		"message-pipeline")
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, "Status: active",
		"durable", "get", "message-pipeline")

	started := runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "start", "message-pipeline", "--name", "cli-local-messages")
	executionID := localModeE2EFieldValue(t, started, "ID")

	waitForLocalModeE2EDurableMessageWait(t, binary, env, dir, executionID, "posted")
	published := runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "messages", "list", "message-pipeline", executionID, "--direction", "out")
	for _, needle := range []string{"progress", `{"stage":"waiting"}`, "Showing 1 message(s)"} {
		requireContains(t, published, needle)
	}

	data := `{"url":"https://example.com/posts/4417"}`
	sent := runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "messages", "send",
		"message-pipeline", executionID, "posted", "--data", data, "--idempotency-key", "post-4417")
	requireContains(t, sent, `Sent "posted" to execution `+executionID)
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "messages", "send",
		"message-pipeline", executionID, "posted", "--data", data, "--idempotency-key", "post-4417"),
		"Already sent with this idempotency key; nothing changed")

	execution := awaitLocalModeE2EDurableExecution(t, binary, env, dir, "message-pipeline", executionID)
	requireContains(t, execution, "Status: succeeded")
	requireContains(t, execution, `"url": "https://example.com/posts/4417"`)

	followed := runVolcanoLocalModeE2E(t, binary, env, dir, "durable", "messages", "list",
		"message-pipeline", executionID, "--direction", "out", "--follow", "--json")
	var stages []string
	for line := range strings.SplitSeq(strings.TrimSpace(followed), "\n") {
		var message apiclient.DurableMessage
		require.NoError(t, json.Unmarshal([]byte(line), &message), followed)
		var payload struct {
			Stage string `json:"stage"`
		}
		require.NoError(t, json.Unmarshal(message.Data, &payload), line)
		stages = append(stages, payload.Stage)
	}
	assert.Equal(t, []string{"waiting", "posted"}, stages, "a follow of an ended execution reads what it published, in order")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "messages", "list", "message-pipeline", executionID), "Showing 3 message(s)")
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir,
		"durable", "messages", "waits", "message-pipeline", executionID),
		"Execution "+executionID+" is not waiting for a message")
	ended, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
		"durable", "messages", "send", "message-pipeline", executionID, "posted")
	require.Error(t, err, "an ended execution has to refuse a message\n%s", ended)
	requireContains(t, ended, "execution "+executionID+" has ended and takes no more messages")
	requireContains(t, ended, "to retry this send safely")
}

// waitForLocalModeE2EDurableMessageWait polls until the execution waits for a
// message with the name.
func waitForLocalModeE2EDurableMessageWait(
	t *testing.T, binary string, env []string, dir, executionID, name string,
) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		var err error
		last, err = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
			"durable", "messages", "waits", "message-pipeline", executionID, "--json")
		if err == nil {
			var waits apiclient.DurableMessageWaitList
			require.NoError(t, json.Unmarshal([]byte(last), &waits), last)
			for _, wait := range waits.Data {
				if wait.Name == name && wait.Status == apiclient.DurableMessageWaitStatusWaiting {
					return
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("execution %s never waited for a %q message:\n%s", executionID, name, last)
}
