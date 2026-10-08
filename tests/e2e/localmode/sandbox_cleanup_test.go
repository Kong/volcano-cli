package localmode

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLocalModeE2EStopCleanSandboxes(t *testing.T) {
	if os.Getenv("VOLCANO_LOCALMODE_E2E") != "1" {
		t.Skip("set VOLCANO_LOCALMODE_E2E=1 to run disposable Docker acceptance")
	}
	requireDocker(t)
	binary, env, dir := buildLocalModeE2EBinary(t), localModeE2EEnv(t), t.TempDir()
	run := func(args ...string) string { return runVolcanoLocalModeE2EStdout(t, binary, env, dir, args...) }
	docker := func(args ...string) string {
		t.Helper()
		c := exec.CommandContext(t.Context(), "docker", args...)
		c.Env = env
		out, err := c.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	t.Cleanup(func() { _, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "stop", "--clean") })
	run("start")
	contextDir := filepath.Join(dir, "custom")
	require.NoError(t, os.MkdirAll(contextDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "Dockerfile"), []byte("RUN printf cleanup-test > /image-version\nCMD [\"sleep\", \"600\"]\n"), 0o600))
	template := uuid.NewString()
	var deployed struct {
		Deployment struct {
			ID string `json:"id"`
		} `json:"deployment"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("sandboxes", "templates", "deploy", "cleanup-test", "--path", contextDir, "--template", template, "--json")), &deployed))
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	require.NoError(t, waitForLocalSandboxDeployment(ctx, 2*time.Second, func(ctx context.Context) (string, error) {
		return captureLocalModeCommand(ctx, binary, env, dir, "sandboxes", "deployments", "get", template, deployed.Deployment.ID, "--json").stdoutResult()
	}))
	var session struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(run("sandboxes", "run", "--template", template, "--duration", "600", "--json")), &session))
	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, `"state":"running"`, "sandboxes", "get", session.ID, "--json")
	filter := "label=dev.volcano.sandbox.namespace=volcano"
	require.NotEmpty(t, docker("container", "ls", "--quiet", "--filter", filter))
	require.NotEmpty(t, docker("image", "ls", "--quiet", "--filter", filter))
	baseImage := docker("inspect", "--format={{.Image}}", "volcano-server")
	other := docker("create", "--label", "dev.volcano.sandbox.namespace=other-"+uuid.NewString(), baseImage)
	t.Cleanup(func() { _ = exec.Command("docker", "container", "rm", "--force", other).Run() })
	run("stop", "--clean")
	require.Empty(t, docker("container", "ls", "--all", "--quiet", "--filter", filter), "Sandbox containers must be reclaimed")
	require.Empty(t, docker("image", "ls", "--quiet", "--filter", filter), "custom images must be reclaimed")
	require.Empty(t, docker("volume", "ls", "--quiet", "--filter", "label=com.docker.compose.project=volcano"))
	docker("container", "inspect", other)
	docker("image", "inspect", baseImage)
	run("stop", "--clean") // A clean retry also succeeds after the stack is gone.
}
