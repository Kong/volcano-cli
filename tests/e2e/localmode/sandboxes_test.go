package localmode

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func requireLocalModeRunsSandboxes(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	run := func(args ...string) string {
		return runVolcanoLocalModeE2E(t, binary, env, dir, append([]string{"sandboxes"}, args...)...)
	}
	require.Contains(t, run("exec", "--preset", "python3.12", "--", "python", "-c", "print('sandbox-python')"), "sandbox-python")
	require.Contains(t, run("exec", "--preset", "node22", "--", "node", "-e", "console.log('sandbox-node')"), "sandbox-node")
	var session struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("run", "--preset", "python3.12", "--duration", "300", "--json")), &session))
	require.NotEmpty(t, session.ID)
	t.Cleanup(func() {
		_, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "terminate", session.ID)
	})
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, `"state":"running"`, "sandboxes", "get", session.ID, "--json")
	run("exec", session.ID, "--", "sh", "-c", "printf retained > /workspace/value")
	require.Equal(t, "retained", run("files", "read", session.ID, "/workspace/value"))
	shell := exec.CommandContext(t.Context(), binary, "sandboxes", "shell", session.ID)
	shell.Env = env
	shell.Dir = dir
	shell.Stdin = strings.NewReader("cat /workspace/value\nexit\n")
	shellOutput, shellErr := shell.CombinedOutput()
	require.NoError(t, shellErr, string(shellOutput))
	require.Contains(t, string(shellOutput), "retained")
	run("suspend", session.ID)
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, `"state":"suspended"`, "sandboxes", "get", session.ID, "--json")
	run("resume", session.ID)
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, `"state":"running"`, "sandboxes", "get", session.ID, "--json")
	require.Equal(t, "retained", run("files", "read", session.ID, "/workspace/value"))
	run("terminate", session.ID)
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, `"state":"terminated"`, "sandboxes", "get", session.ID, "--json")
}
