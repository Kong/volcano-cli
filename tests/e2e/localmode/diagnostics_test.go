package localmode

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// localModeE2EDiagnosticsDirEnv names the directory a failed test saves the
// local stack's state to. CI uploads it; without it, only short tails are
// logged.
const localModeE2EDiagnosticsDirEnv = "VOLCANO_LOCALMODE_E2E_DIAGNOSTICS_DIR"

const (
	localModeE2EDiagnosticsBudget         = time.Minute
	localModeE2EDiagnosticsCommandTimeout = 20 * time.Second
	localModeE2EServerLogLines            = "1000"
	localModeE2EServerLogTailLines        = 50
	localModeE2ERuntimeLogTailLines       = 30
	localModeE2EFrontendRuntimeDir        = "/app/local-storage/frontend-runtime"
)

// stopLocalModeE2EStack removes the local stack, its containers, and its
// volumes. Failures are ignored: there may be no stack to remove.
func stopLocalModeE2EStack(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	_, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "stop", "--clean")
}

// registerLocalModeE2EStopClean removes the local stack when the test ends. A
// failed test first saves what the stack can still tell, because stop --clean
// deletes the containers and volumes that hold the server and runtime logs.
func registerLocalModeE2EStopClean(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			collectLocalModeE2EDiagnostics(t, env)
		}
		stopLocalModeE2EStack(t, binary, env, dir)
	})
}

// collectLocalModeE2EDiagnostics saves the stack's containers, the server log,
// and every frontend runtime log, and logs short tails of them. It is best
// effort: nothing here fails the test or hides why it failed.
func collectLocalModeE2EDiagnostics(t *testing.T, env []string) {
	t.Helper()
	// t.Context() is already cancelled when cleanups run.
	ctx, cancel := context.WithTimeout(context.Background(), localModeE2EDiagnosticsBudget)
	defer cancel()

	saveDir, kept, err := localModeE2EDiagnosticsDir(t.Name())
	if err != nil {
		t.Logf("Local stack diagnostics: %v", err)
		return
	}
	if kept {
		t.Logf("Local stack diagnostics saved to %s", saveDir)
	} else {
		defer os.RemoveAll(saveDir)
		t.Logf("Local stack diagnostics (set %s to keep the full files):", localModeE2EDiagnosticsDirEnv)
	}

	ps, err := localModeE2EDocker(ctx, env, "compose", "-p", "volcano", "--profile", "sandboxes", "ps", "-a")
	saveLocalModeE2EDiagnostic(t, saveDir, "compose-ps.txt", ps)
	t.Logf("docker compose ps -a (err: %v):\n%s", err, ps)

	serverLog, err := localModeE2EDocker(ctx, env, "logs", "--tail", localModeE2EServerLogLines, "volcano-server")
	saveLocalModeE2EDiagnostic(t, saveDir, "volcano-server.log", serverLog)
	t.Logf("volcano-server log, last %d lines (err: %v):\n%s",
		localModeE2EServerLogTailLines, err, lastLocalModeE2ELines(serverLog, localModeE2EServerLogTailLines))

	// docker cp needs no shell in the image and reaches the logs of previews,
	// which run under their own ID beside the frontends.
	runtimeDir := filepath.Join(saveDir, "frontend-runtime")
	// docker cp copies into an existing directory rather than replacing it, so
	// clear one an earlier failed run left in a reused diagnostics directory.
	if err := os.RemoveAll(runtimeDir); err != nil {
		t.Logf("Could not clear %s: %v", runtimeDir, err)
		return
	}
	if output, err := localModeE2EDocker(ctx, env, "cp", "volcano-server:"+localModeE2EFrontendRuntimeDir, runtimeDir); err != nil {
		t.Logf("No frontend runtime logs copied: %v\n%s", err, output)
		return
	}
	logLocalModeE2ERuntimeLogs(t, runtimeDir)
}

// localModeE2EDiagnosticsDir returns where to save a test's diagnostics and
// whether the files are kept after the test.
func localModeE2EDiagnosticsDir(testName string) (string, bool, error) {
	root := strings.TrimSpace(os.Getenv(localModeE2EDiagnosticsDirEnv))
	if root == "" {
		dir, err := os.MkdirTemp("", "localmode-e2e-diagnostics-*")
		return dir, false, err
	}
	// Subtest names contain slashes, which nest under the parent test.
	dir := filepath.Join(root, filepath.FromSlash(testName))
	return dir, true, os.MkdirAll(dir, 0o755)
}

func localModeE2EDocker(ctx context.Context, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, localModeE2EDiagnosticsCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func saveLocalModeE2EDiagnostic(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Logf("Could not save %s: %v", name, err)
	}
}

func logLocalModeE2ERuntimeLogs(t *testing.T, runtimeDir string) {
	t.Helper()
	found := false
	err := filepath.WalkDir(runtimeDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != "runtime.log" {
			return err
		}
		found = true
		content, err := os.ReadFile(path)
		if err != nil {
			t.Logf("Could not read %s: %v", path, err)
			return nil
		}
		name, _ := filepath.Rel(runtimeDir, path)
		t.Logf("frontend-runtime/%s, last %d lines:\n%s",
			filepath.ToSlash(name), localModeE2ERuntimeLogTailLines, lastLocalModeE2ELines(string(content), localModeE2ERuntimeLogTailLines))
		return nil
	})
	if err != nil {
		t.Logf("Could not list frontend runtime logs: %v", err)
	}
	if !found {
		t.Logf("No frontend runtime logs in %s", localModeE2EFrontendRuntimeDir)
	}
}

// lastLocalModeE2ELines returns the last n lines of text.
func lastLocalModeE2ELines(text string, n int) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func TestLastLocalModeE2ELines(t *testing.T) {
	require.Empty(t, lastLocalModeE2ELines("", 3))
	require.Equal(t, "a\nb", lastLocalModeE2ELines("a\nb\n", 3))
	require.Equal(t, "c\nd", lastLocalModeE2ELines("a\nb\nc\nd\n", 2))
}

func TestLocalModeE2EDiagnosticsDir(t *testing.T) {
	t.Run("keeps files under the named directory, nesting subtests", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv(localModeE2EDiagnosticsDirEnv, root)
		dir, kept, err := localModeE2EDiagnosticsDir("TestLocalModeE2EStopCleanSandboxes/graceful")
		require.NoError(t, err)
		require.True(t, kept)
		require.Equal(t, filepath.Join(root, "TestLocalModeE2EStopCleanSandboxes", "graceful"), dir)
		require.DirExists(t, dir)
	})
	t.Run("uses a temporary directory without it", func(t *testing.T) {
		t.Setenv(localModeE2EDiagnosticsDirEnv, "")
		dir, kept, err := localModeE2EDiagnosticsDir("TestLocalModeE2ESmoke")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		require.False(t, kept)
		require.DirExists(t, dir)
	})
}
