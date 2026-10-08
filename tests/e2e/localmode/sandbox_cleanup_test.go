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
	binary := buildLocalModeE2EBinary(t)
	for _, interrupted := range []bool{false, true} {
		name := "graceful"
		if interrupted {
			name = "interrupted-broker"
		}
		t.Run(name, func(t *testing.T) { testStopCleanSandboxes(t, binary, interrupted) })
	}
}

func testStopCleanSandboxes(t *testing.T, binary string, interrupted bool) {
	t.Helper()
	env, dir := localModeE2EEnv(t), t.TempDir()
	run := func(args ...string) string { return runVolcanoLocalModeE2EStdout(t, binary, env, dir, args...) }
	docker := func(args ...string) string {
		t.Helper()
		c := exec.CommandContext(t.Context(), "docker", args...)
		c.Env = env
		out, err := c.CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}
	cleanupDocker := func(args ...string) {
		t.Cleanup(func() {
			c := exec.Command("docker", args...)
			c.Env = env
			_ = c.Run()
		})
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
	require.NotEmpty(t, docker("network", "ls", "--quiet", "--filter", filter))
	serverImage := docker("inspect", "--format={{.Image}}", "volcano-server")
	// Hosting's LocalCustomBaseImage pins the Amazon Linux build base, which
	// is separate from the server/broker image. It must survive cleanup too.
	const customBaseImage = "public.ecr.aws/amazonlinux/amazonlinux@sha256:06da5a3362eda00c5114227ac81abe56dd9b943395553b7c64a310457f4d9e2b"
	baseID := docker("image", "inspect", "--format={{.Id}}", customBaseImage)
	otherLabel := "dev.volcano.sandbox.namespace=other-" + uuid.NewString()
	other := docker("create", "--label", otherLabel, serverImage)
	cleanupDocker("container", "rm", "--force", other)
	otherNetwork := docker("network", "create", "--label", otherLabel, "cleanup-other-"+uuid.NewString())
	cleanupDocker("network", "rm", otherNetwork)
	otherContext := filepath.Join(dir, "other-image")
	require.NoError(t, os.MkdirAll(otherContext, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(otherContext, "Dockerfile"), []byte("FROM scratch\nLABEL purpose=cleanup-preservation-test\n"), 0o600))
	otherTag := "cleanup-other:" + uuid.NewString()
	docker("build", "--label", otherLabel, "--tag", otherTag, otherContext)
	cleanupDocker("image", "rm", otherTag)
	otherImage := docker("image", "inspect", "--format={{.Id}}", otherTag)
	if interrupted {
		broker := docker("container", "ls", "--quiet", "--filter", "label=com.docker.compose.project=volcano", "--filter", "label=com.docker.compose.service=sandbox-broker")
		require.NotEmpty(t, broker)
		docker("update", "--restart=no", broker)
		docker("kill", "--signal=KILL", broker)
		// Prove there are orphaned resources before invoking the CLI fallback.
		require.NotEmpty(t, docker("container", "ls", "--all", "--quiet", "--filter", filter))
		require.NotEmpty(t, docker("network", "ls", "--quiet", "--filter", filter))
	}
	run("stop", "--clean")
	require.Empty(t, docker("container", "ls", "--all", "--quiet", "--filter", filter), "Sandbox containers must be reclaimed")
	require.Empty(t, docker("image", "ls", "--quiet", "--filter", filter), "custom images must be reclaimed")
	require.Empty(t, docker("network", "ls", "--quiet", "--filter", filter), "private networks must be reclaimed")
	require.Empty(t, docker("volume", "ls", "--quiet", "--filter", "label=com.docker.compose.project=volcano"))
	docker("container", "inspect", other)
	docker("network", "inspect", otherNetwork)
	docker("image", "inspect", serverImage)
	require.Equal(t, baseID, docker("image", "inspect", "--format={{.Id}}", customBaseImage))
	require.Equal(t, otherImage, docker("image", "inspect", "--format={{.Id}}", otherTag))
	run("stop", "--clean") // A clean retry also succeeds after the stack is gone.
}
