package localmode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type localModeCommandResult struct {
	stdout string
	stderr string
	err    error
}

func captureLocalModeCommand(ctx context.Context, binary string, env []string, dir string, args ...string) localModeCommandResult {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = env
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return localModeCommandResult{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func (r localModeCommandResult) stdoutResult() (string, error) {
	if r.err != nil {
		return r.stdout, fmt.Errorf("CLI command failed: %w\n%s", r.err, r.stderr)
	}
	return r.stdout, nil
}

func runVolcanoLocalModeE2EStdout(t *testing.T, binary string, env []string, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	result := captureLocalModeCommand(ctx, binary, env, dir, args...)
	stdout, err := result.stdoutResult()
	require.NoError(t, err, stdout)
	return stdout
}

func TestCaptureLocalModeCommand(t *testing.T) {
	t.Run("keeps JSON separate from deployment diagnostics", func(t *testing.T) {
		result := captureLocalModeCommand(t.Context(), "/bin/sh", nil, t.TempDir(), "-c",
			`printf 'Template ID: example\nRequest ID: request\n' >&2; printf '{"deployment":{"id":"accepted"}}'`)
		stdout, err := result.stdoutResult()
		require.NoError(t, err)
		require.True(t, json.Valid([]byte(stdout)))
		require.Contains(t, result.stderr, "Template ID: example")
		require.Contains(t, result.stderr, "Request ID: request")
	})
	t.Run("preserves arbitrary binary stdout", func(t *testing.T) {
		result := captureLocalModeCommand(t.Context(), "/bin/sh", nil, t.TempDir(), "-c", `printf '\000\377\037\213'; printf 'download diagnostic' >&2`)
		stdout, err := result.stdoutResult()
		require.NoError(t, err)
		require.Equal(t, []byte{0, 255, 31, 139}, []byte(stdout))
		require.Equal(t, "download diagnostic", result.stderr)
	})
	t.Run("preserves failure diagnostics without contaminating stdout", func(t *testing.T) {
		result := captureLocalModeCommand(t.Context(), "/bin/sh", nil, t.TempDir(), "-c", `printf 'partial'; printf 'build failed' >&2; exit 1`)
		stdout, err := result.stdoutResult()
		require.Equal(t, "partial", stdout)
		require.ErrorContains(t, err, "build failed")
	})
}
