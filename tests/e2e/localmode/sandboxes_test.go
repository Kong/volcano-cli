package localmode

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

func requireLocalModeRunsSandboxes(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	run := func(args ...string) string {
		return runVolcanoLocalModeE2EStdout(t, binary, env, dir, append([]string{"sandboxes"}, args...)...)
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
	requireLocalModeUploadsSandboxDirectory(t, binary, env, dir, session.ID)
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
	requireLocalModeCustomSandbox(t, binary, env, dir)
}

// requireLocalModeUploadsSandboxDirectory copies a directory into a running
// session and reads it back, then checks that a conflicting upload writes
// nothing.
func requireLocalModeUploadsSandboxDirectory(t *testing.T, binary string, env []string, dir, session string) {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		return runVolcanoLocalModeE2EStdout(t, binary, env, dir, append([]string{"sandboxes"}, args...)...)
	}
	tree := filepath.Join(t.TempDir(), "upload")
	require.NoError(t, os.MkdirAll(filepath.Join(tree, "src", "empty"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(tree, "README.md"), []byte("uploaded-readme\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(tree, "src", "run.sh"), []byte("#!/bin/sh\necho uploaded-script\n"), 0o755))

	var totals struct {
		Files int64 `json:"files"`
		Bytes int64 `json:"bytes"`
		Calls int   `json:"calls"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("files", "upload", session, tree, "--path", "uploaded", "--json")), &totals))
	require.Equal(t, int64(2), totals.Files)
	require.Equal(t, int64(len("uploaded-readme\n#!/bin/sh\necho uploaded-script\n")), totals.Bytes)
	require.Equal(t, 1, totals.Calls)
	require.Equal(t, "uploaded-readme\n", run("files", "read", session, "/workspace/uploaded/README.md"))
	// The local workspace is mounted noexec, so the mode is read rather than
	// the script run directly.
	require.Equal(t, "755\n", run("exec", session, "--", "stat", "-c", "%a", "/workspace/uploaded/src/run.sh"), "keeps the executable bit")
	require.Equal(t, "uploaded-script\n", run("exec", session, "--", "sh", "/workspace/uploaded/src/run.sh"))
	run("exec", session, "--", "test", "-d", "/workspace/uploaded/src/empty")

	// README.md is a directory where the upload has a file, so the whole
	// archive is refused: src is not created either.
	run("exec", session, "--", "mkdir", "-p", "/workspace/conflict/README.md")
	output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "files", "upload", session, tree, "--path", "/workspace/conflict")
	require.Error(t, err, output)
	require.Contains(t, output, "the upload conflicts with the session's workspace")
	require.Contains(t, output, "Nothing was written")
	run("exec", session, "--", "test", "!", "-e", "/workspace/conflict/src")

	// Incompressible and just over the 32 MiB one archive call takes, so the
	// file goes on its own in five parts, after an empty archive.
	large := filepath.Join(t.TempDir(), "large")
	require.NoError(t, os.MkdirAll(large, 0o755))
	blob := make([]byte, 33<<20)
	_, err = rand.Read(blob)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(large, "blob.bin"), blob, 0o644))
	require.NoError(t, json.Unmarshal([]byte(run("files", "upload", session, large, "--path", "large", "--json")), &totals))
	require.Equal(t, int64(1), totals.Files)
	require.Equal(t, int64(len(blob)), totals.Bytes)
	require.Equal(t, 6, totals.Calls)
	sum := sha256.Sum256(blob)
	require.Equal(t, hex.EncodeToString(sum[:])+"  /workspace/large/blob.bin\n", run("exec", session, "--", "sha256sum", "/workspace/large/blob.bin"))
	require.Equal(t, "600\n", run("exec", session, "--", "stat", "-c", "%a", "/workspace/large/blob.bin"), "parted files land 0600")
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

func requireLocalModeCustomSandbox(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	contextDir := filepath.Join(dir, "custom-sandbox")
	require.NoError(t, os.MkdirAll(contextDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("RUN dnf install -y python3.12 && dnf clean all\nRUN printf custom-image > /image-version\nCMD [\"python3.12\", \"-m\", \"http.server\", \"8080\", \"--bind\", \"0.0.0.0\"]\n"), 0o600))
	template := uuid.NewString()
	defer func() {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "templates", "delete", template, "--yes")
		require.NoError(t, err, output)
	}()
	run := func(args ...string) string {
		t.Helper()
		return runVolcanoLocalModeE2EStdout(t, binary, env, dir, append([]string{"sandboxes"}, args...)...)
	}
	var result struct {
		Deployment struct {
			ID string `json:"id"`
		} `json:"deployment"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("templates", "deploy", "local-custom", "--path", contextDir, "--template", template, "--ports", "8080", "--json")), &result))
	require.NotEmpty(t, result.Deployment.ID)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	require.NoError(t, waitForLocalSandboxDeployment(ctx, 2*time.Second, func(ctx context.Context) (string, error) {
		return captureLocalModeCommand(ctx, binary, env, dir,
			"sandboxes", "deployments", "get", template, result.Deployment.ID, "--json").stdoutResult()
	}))
	require.Contains(t, run("deployments", "list", template), result.Deployment.ID)
	require.Contains(t, run("exec", "--template", template, "--", "cat", "/image-version"), "custom-image")
	requireLocalSandboxDeploymentArtifacts(t, run, template, result.Deployment.ID)
	requireLocalSandboxConfigRoundTrip(t, binary, env, dir, template)
}
