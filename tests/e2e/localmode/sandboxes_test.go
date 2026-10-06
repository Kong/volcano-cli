package localmode

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
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
	requireLocalModeSandboxUsage(t, binary, env, dir)
}

func requireLocalModeSandboxUsage(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	info := fetchVolcanoLocalModeE2EInfo(t, env)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
		strings.TrimRight(info.APIURL, "/")+"/projects/"+info.ProjectID+"/usage", http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+info.UserToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var serverUsage apiclient.ProjectUsageResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&serverUsage))
	names := []string{"Sandbox Running (MiB-Seconds)", "Sandbox Suspended (Seconds)", "Sandbox Uncertain (MiB-Seconds)"}
	var serverNames []string
	for _, metric := range serverUsage.Metrics {
		if slices.Contains(names, metric.Metric) {
			serverNames = append(serverNames, metric.Metric)
		}
	}
	output, usageErr := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "usage", "--json")
	if len(serverNames) == 0 {
		// Published images can predate metering; Hosting's candidate-image lane must expose it.
		require.NotEqual(t, "1", os.Getenv("VOLCANO_E2E_REQUIRE_SANDBOX_USAGE"), "candidate image must expose Sandbox preview usage")
		require.Error(t, usageErr)
		require.Contains(t, output, "this server does not expose Sandbox preview usage yet")
		return
	}
	require.ElementsMatch(t, names, serverNames, "server must expose the complete preview contract")
	require.NoError(t, usageErr, output)
	var usage apiclient.ProjectUsageResponse
	require.NoError(t, json.Unmarshal([]byte(output), &usage))
	var actualNames []string
	for _, m := range usage.Metrics {
		actualNames = append(actualNames, m.Metric)
		require.Zero(t, m.AllTime)
		require.Zero(t, m.Total)
	}
	require.ElementsMatch(t, names, actualNames)
}
