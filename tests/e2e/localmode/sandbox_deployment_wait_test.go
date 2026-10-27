package localmode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func requireLocalSandboxDeployment(t *testing.T, binary string, env []string, dir, template, deployment string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	err := waitForLocalSandboxDeployment(ctx, 2*time.Second, func(ctx context.Context) (string, error) {
		return captureLocalModeCommand(ctx, binary, env, dir,
			"sandboxes", "deployments", "get", template, deployment, "--json").stdoutResult()
	})
	if err != nil {
		// Collect before FailNow runs deferred template deletion and stack cleanup.
		logLocalSandboxDeploymentFailure(t, binary, env, dir, template, deployment)
	}
	require.NoError(t, err)
}

func logLocalSandboxDeploymentFailure(t *testing.T, binary string, env []string, dir, template, deployment string) {
	t.Helper()
	t.Logf("Sandbox deployment diagnostics: template=%s deployment=%s", template, deployment)
	// The polling context may have expired. Keep diagnostics bounded independently
	// and reserve time for container logs even when the build-log API is unavailable.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 30*time.Second)
	defer cancel()
	buildCtx, cancelBuild := context.WithTimeout(ctx, 15*time.Second)
	logLocalSandboxBuildFailure(t, buildCtx, binary, env, dir, template, deployment)
	cancelBuild()
	logSandboxDiagnosticCommand(t, ctx, "docker", env, dir, "logs", "--timestamps", "--tail", "200", "volcano-server")
	brokers := logSandboxDiagnosticCommand(t, ctx, "docker", env, dir,
		"container", "ls", "--all", "--quiet", "--filter", "label=com.docker.compose.project=volcano",
		"--filter", "label=com.docker.compose.service=sandbox-broker")
	for _, broker := range strings.Fields(brokers.stdout) {
		logSandboxDiagnosticCommand(t, ctx, "docker", env, dir, "logs", "--timestamps", "--tail", "200", broker)
	}
}

func logLocalSandboxBuildFailure(t *testing.T, ctx context.Context, binary string, env []string, dir, template, deployment string) {
	t.Helper()
	cursor := ""
	seen := make(map[string]bool)
	for page := 0; page < 10 && ctx.Err() == nil; page++ {
		args := []string{"sandboxes", "deployments", "logs", template, deployment, "--region", "us-east-1", "--limit", "1000", "--json"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		result := logSandboxDiagnosticCommand(t, ctx, binary, env, dir, args...)
		if result.err != nil {
			return
		}
		var logs struct {
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(result.stdout), &logs); err != nil {
			t.Logf("Could not decode build-log continuation: %v", err)
			return
		}
		if logs.NextCursor == "" {
			return
		}
		if seen[logs.NextCursor] {
			t.Logf("Build logs incomplete: repeated cursor %q", logs.NextCursor)
			return
		}
		cursor = logs.NextCursor
		seen[cursor] = true
	}
	t.Logf("Build logs incomplete after diagnostic budget: cursor=%q error=%v", cursor, ctx.Err())
}

func logSandboxDiagnosticCommand(t *testing.T, ctx context.Context, binary string, env []string, dir string, args ...string) localModeCommandResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	result := captureLocalModeCommand(ctx, binary, env, dir, args...)
	t.Logf("%s: %v\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), result.err,
		sandboxDiagnosticTail(result.stdout), sandboxDiagnosticTail(result.stderr))
	return result
}

func sandboxDiagnosticTail(output string) string {
	const limit = 64 << 10
	if len(output) > limit {
		return "[earlier diagnostic output truncated]\n" + output[len(output)-limit:]
	}
	return output
}

// waitForLocalSandboxDeployment bounds cold builds and stops on terminal failures.
func waitForLocalSandboxDeployment(ctx context.Context, interval time.Duration, read func(context.Context) (string, error)) error {
	var output string
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sandbox deployment did not activate: %w\n%s", errors.Join(err, readErr), output)
		}
		output, readErr = read(ctx)
		if readErr == nil {
			var deployment struct {
				Status string `json:"status"`
			}
			if err := json.Unmarshal([]byte(output), &deployment); err != nil {
				return fmt.Errorf("invalid sandbox deployment response: %w", err)
			}
			switch deployment.Status {
			case "active":
				return nil
			case "failed", "retired", "deleted":
				return fmt.Errorf("sandbox deployment reached terminal state %q\n%s", deployment.Status, output)
			}
		}
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
	}
}

func TestWaitForLocalSandboxDeployment(t *testing.T) {
	t.Run("polls transient failures and builds until active", func(t *testing.T) {
		calls := 0
		err := waitForLocalSandboxDeployment(t.Context(), 0, func(context.Context) (string, error) {
			calls++
			switch calls {
			case 1:
				return "unavailable", errors.New("temporary read failure")
			case 2:
				return `{"status":"building"}`, nil
			default:
				return `{"status":"active"}`, nil
			}
		})
		require.NoError(t, err)
		require.Equal(t, 3, calls)
	})
	for _, status := range []string{"failed", "retired", "deleted"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			err := waitForLocalSandboxDeployment(t.Context(), time.Hour, func(context.Context) (string, error) {
				calls++
				return fmt.Sprintf(`{"status":%q}`, status), nil
			})
			require.ErrorContains(t, err, "terminal state")
			require.ErrorContains(t, err, status)
			require.Equal(t, 1, calls)
		})
	}
	t.Run("rejects malformed JSON immediately", func(t *testing.T) {
		err := waitForLocalSandboxDeployment(t.Context(), time.Hour, func(context.Context) (string, error) {
			return "not JSON", nil
		})
		require.ErrorContains(t, err, "invalid sandbox deployment response")
	})
	t.Run("cancels without waiting for another poll", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		err := waitForLocalSandboxDeployment(ctx, time.Hour, func(context.Context) (string, error) {
			calls++
			cancel()
			return `{"status":"building"}`, nil
		})
		require.ErrorIs(t, err, context.Canceled)
		require.ErrorContains(t, err, "building")
		require.Equal(t, 1, calls)
	})
}
