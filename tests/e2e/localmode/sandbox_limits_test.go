package localmode

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// requireLocalModeSandboxLimits checks that --vcpus and --disk-mib hold inside
// the session, and that the session reports them applied once it is running.
func requireLocalModeSandboxLimits(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		return runVolcanoLocalModeE2EStdout(t, binary, env, dir, append([]string{"sandboxes"}, args...)...)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	create := captureLocalModeCommand(ctx, binary, env, dir, "sandboxes", "run", "--preset", "python3.12", "--duration", "300", "--vcpus", "1", "--disk-mib", "256", "--json")
	if create.err != nil {
		// Published images can predate limits; Hosting's candidate-image lane must accept them.
		require.NotEqual(t, "1", os.Getenv("VOLCANO_E2E_REQUIRE_SANDBOX_LIMITS"), "candidate image must accept Sandbox limits: %s", create.stderr)
		require.Contains(t, create.stderr, "HTTP 400")
		require.Contains(t, create.stderr, "limits")
		return
	}
	var created apiclient.SandboxSession
	require.NoError(t, json.Unmarshal([]byte(create.stdout), &created))
	require.NotEqual(t, uuid.Nil, created.Id)
	id := created.Id.String()
	t.Cleanup(func() {
		_, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "terminate", id)
	})
	require.NotNil(t, created.Limits, "a session created with caps reports limits")

	waitForVolcanoLocalModeE2EContains(t, binary, env, dir, `"state":"running"`, "sandboxes", "get", id, "--json")
	var session apiclient.SandboxSession
	require.NoError(t, json.Unmarshal([]byte(run("get", id, "--json")), &session))
	require.Equal(t, apiclient.SandboxSessionStateRunning, session.State)
	require.NotNil(t, session.Limits)
	applied, err := session.Limits.Applied.AsSandboxSessionAppliedLimitsApplied0()
	require.NoError(t, err, "a running session reports applied as true or false")
	require.True(t, applied)
	require.NotNil(t, session.Limits.Vcpus)
	require.Equal(t, 1, *session.Limits.Vcpus)
	require.NotNil(t, session.Limits.DiskMib)
	require.Equal(t, 256, *session.Limits.DiskMib)

	require.Equal(t, "100000 100000", strings.TrimSpace(run("exec", id, "--", "cat", "/sys/fs/cgroup/cpu.max")))

	fill := captureLocalModeCommand(ctx, binary, env, dir, "sandboxes", "exec", id, "--", "sh", "-c", "head -c 300M /dev/zero > /workspace/fill")
	require.Error(t, fill.err, "writing past --disk-mib must fail\nstdout:\n%s\nstderr:\n%s", fill.stdout, fill.stderr)
	require.Contains(t, fill.stderr, "No space left on device")
	run("terminate", id)
}
