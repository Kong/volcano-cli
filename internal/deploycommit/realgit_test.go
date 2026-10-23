package deploycommit

// These tests run the installed git, so the arguments Detect passes and the
// blob IDs Commit computes are checked against git itself rather than
// against a fake that shares the code's assumptions.

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

const helloPath = "volcano/functions/hello.js"

// newRepository makes a repository with one commit and a GitHub origin, away
// from the machine's own git configuration. git runs a git command in it.
func newRepository(t *testing.T) (root string, git func(args ...string) string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for name, value := range map[string]string{
		OptOutEnv:             "",
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "Test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_COMMITTER_NAME":  "Test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
	} {
		t.Setenv(name, value)
	}

	root = t.TempDir()
	git = func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
		require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
		return strings.TrimSpace(string(out))
	}
	git("init", "--quiet")
	git("remote", "add", "origin", "https://github.com/acme/app.git")
	writeFile(t, root, helloPath, "exports.handler = 1\n")
	writeFile(t, root, ".gitignore", ".env.production\n")
	git("add", ".")
	git("commit", "--quiet", "-m", "initial")
	return root, git
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
}

// packFromDisk archives files as they are on disk, as the packagers do.
func packFromDisk(t *testing.T, root string, paths ...string) []byte {
	t.Helper()
	files := map[string]string{}
	for _, path := range paths {
		content, err := os.ReadFile(filepath.Join(root, path))
		require.NoError(t, err)
		files[path] = string(content)
	}
	return archive(t, files)
}

func TestCommitWithRealGit(t *testing.T) {
	root, git := newRepository(t)

	// A deploy can run from any directory inside the repository.
	source := Detect(context.Background(), cliruntime.Deps{}, filepath.Join(root, "volcano"))
	got := source.Commit(packFromDisk(t, root, helloPath))
	require.NotNil(t, got)
	assert.Equal(t, "acme/app", got.Repository)
	assert.Equal(t, git("rev-parse", "HEAD"), got.SHA)
	assert.False(t, got.Dirty)

	// git status lists neither of these, but both change what deploys.
	writeFile(t, root, ".env.production", "API_URL=https://example.com\n")
	assert.True(t, source.Commit(packFromDisk(t, root, helloPath, ".env.production")).Dirty, "an ignored file")

	git("update-index", "--assume-unchanged", helloPath)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	assert.Empty(t, git("status", "--porcelain"))
	assert.True(t, source.Commit(packFromDisk(t, root, helloPath)).Dirty, "an assume-unchanged edit")
}

// A workflow can check out a ref other than the one that triggered it; the
// commit is whatever the deploy directory holds.
func TestCommitWithRealGitReportsTheCheckedOutCommit(t *testing.T) {
	root, git := newRepository(t)
	first := git("rev-parse", "HEAD")
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	git("commit", "--quiet", "-am", "second")
	git("checkout", "--quiet", "--detach", first)

	got := Detect(context.Background(), cliruntime.Deps{}, root).Commit(packFromDisk(t, root, helloPath))
	require.NotNil(t, got)
	assert.Equal(t, first, got.SHA)
	assert.False(t, got.Dirty)
}

func TestDetectWithRealGitOutsideARepository(t *testing.T) {
	_, _ = newRepository(t)
	assert.Nil(t, Detect(context.Background(), cliruntime.Deps{}, t.TempDir()))
}
