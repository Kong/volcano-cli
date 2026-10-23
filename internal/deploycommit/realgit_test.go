package deploycommit

// These tests run the installed git, so the arguments Detect passes are
// checked against git itself rather than against a fake that shares the
// code's assumptions.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// newRepository makes a repository with one commit and a GitHub origin, away
// from the machine's own git configuration.
func newRepository(t *testing.T) (root, head string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	clearEnv(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	root = t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	git("remote", "add", "origin", "https://github.com/acme/app.git")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "volcano", "functions"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "volcano", "functions", "hello.js"), []byte("exports.handler = 1\n"), 0o644))
	git("add", ".")
	git("commit", "--quiet", "-m", "initial")
	return root, git("rev-parse", "HEAD")
}

func TestDetectWithRealGit(t *testing.T) {
	root, head := newRepository(t)

	// A deploy can run from any directory inside the repository.
	got := Detect(context.Background(), cliruntime.Deps{}, filepath.Join(root, "volcano"))
	require.NotNil(t, got)
	assert.Equal(t, "acme/app", got.Repository)
	assert.Equal(t, head, got.SHA)
	assert.False(t, got.Dirty)

	require.NoError(t, os.WriteFile(filepath.Join(root, "volcano", "functions", "hello.js"), []byte("exports.handler = 2\n"), 0o644))
	got = Detect(context.Background(), cliruntime.Deps{}, root)
	require.NotNil(t, got)
	assert.True(t, got.Dirty)
}

func TestDetectWithRealGitOutsideARepository(t *testing.T) {
	_, _ = newRepository(t)
	assert.Nil(t, Detect(context.Background(), cliruntime.Deps{}, t.TempDir()))
}
