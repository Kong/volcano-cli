// Package deploycommittest makes git repositories for tests of deploys that
// report their source commit.
package deploycommittest

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Repository makes root a git repository whose origin is remote, with one
// commit that holds every file in root that git does not ignore, and returns
// that commit's ID. git runs with none of the machine's own configuration,
// for the rest of the test too. The test is skipped when git is not
// installed.
func Repository(t *testing.T, root, remote string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	for name, value := range map[string]string{
		"VOLCANO_DEPLOY_COMMIT": "",
		"GIT_CONFIG_GLOBAL":     os.DevNull,
		"GIT_CONFIG_NOSYSTEM":   "1",
		"GIT_AUTHOR_NAME":       "Test",
		"GIT_AUTHOR_EMAIL":      "test@example.com",
		"GIT_COMMITTER_NAME":    "Test",
		"GIT_COMMITTER_EMAIL":   "test@example.com",
	} {
		t.Setenv(name, value)
	}
	Git(t, root, "init", "--quiet")
	Git(t, root, "remote", "add", "origin", remote)
	Git(t, root, "add", ".")
	Git(t, root, "commit", "--quiet", "-m", "initial")
	return Git(t, root, "rev-parse", "HEAD")
}

// Git runs git in root and returns its trimmed output.
func Git(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", root}, args...)...).CombinedOutput() //nolint:gosec // a test runs git in its own repository
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}
