package localgit

// The tests in this file run the real git binary. Everything else in the package
// runs against a fake runner, and a fake has twice modelled git wrongly here: an
// unset config key returning a plain error rather than exit status 1, and `git
// branch --show-current` on a detached HEAD reported as a failure instead of the
// empty output git actually prints. Worse, the push routing was written believing
// these keys hold a remote name — git takes a URL just as happily. A fake cannot
// catch either class of mistake, because the fake encodes the same belief the
// code does.

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

// realGitClient returns a Client that runs the installed git inside dir. The
// runner is the injection point, so nothing has to change directory and these
// tests stay parallel-safe.
func realGitClient(t *testing.T, dir string) Client {
	t.Helper()
	requireGit(t)
	return Client{runner: cliruntime.CommandRunnerFunc(
		func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return gitCommand(ctx, dir, name, args...).Output()
		})}
}

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
}

// gitCommand isolates the run from the machine it runs on. A developer's global
// config can set init.defaultBranch or a push default of its own, and either
// would decide the outcome of a test about which setting decides the outcome.
func gitCommand(ctx context.Context, dir, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
	)
	return cmd
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCommand(t.Context(), dir, "git", args...).CombinedOutput()
	require.NoErrorf(t, err, "git %s: %s", strings.Join(args, " "), out)
	return strings.TrimSpace(string(out))
}

// checkout is a work repository with two bare repositories to push to, and a
// third that no remote names.
type checkout struct {
	dir     string
	branch  string
	origin  string
	fork    string
	unnamed string
	client  Client
}

func newCheckout(t *testing.T) checkout {
	t.Helper()
	requireGit(t)
	root := t.TempDir()

	bare := func(name string) string {
		path := filepath.Join(root, name)
		require.NoError(t, os.MkdirAll(path, 0o755))
		git(t, path, "init", "--quiet", "--bare")
		return path
	}
	origin, fork, unnamed := bare("origin.git"), bare("fork.git"), bare("unnamed.git")

	work := filepath.Join(root, "work")
	require.NoError(t, os.MkdirAll(work, 0o755))
	git(t, work, "init", "--quiet")
	git(t, work, "config", "user.email", "test@volcano.test")
	git(t, work, "config", "user.name", "Test")
	// push.default decides the refspec, not the remote. Pinning it keeps a bare
	// `git push` from failing for want of an upstream branch, so that what the
	// test observes is which repository git routed to.
	git(t, work, "config", "push.default", "current")
	git(t, work, "remote", "add", "origin", origin)
	git(t, work, "remote", "add", "fork", fork)

	return checkout{
		dir: work, branch: git(t, work, "branch", "--show-current"),
		origin: origin, fork: fork, unnamed: unnamed,
		client: realGitClient(t, work),
	}
}

// commitAndPush makes a commit, runs a bare `git push`, and reports the commit
// so a caller can ask which repository received it.
func (c checkout) commitAndPush(t *testing.T, message string) string {
	t.Helper()
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", message)
	git(t, c.dir, "push", "--quiet")
	return git(t, c.dir, "rev-parse", "HEAD")
}

func (c checkout) received(t *testing.T, bare, commit string) bool {
	t.Helper()
	out, err := gitCommand(t.Context(), bare, "git", "rev-parse", c.branch).Output()
	if err != nil {
		return false // the branch does not exist there, so nothing was pushed
	}
	return strings.TrimSpace(string(out)) == commit
}

// The precedence PushRemote implements is git's, and this asserts it by watching
// which repository a bare `git push` actually updates — not by re-reading the
// documentation the implementation was written from.
//
// Each case sets every key it cares about and gets its own checkout, so a case
// asserting that one key outranks another cannot pass on a leftover from the one
// before it.
func TestPushRemoteAgreesWithRealGitAboutPrecedence(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		// set is keyed by config key, with "<branch>" standing in for the
		// current branch's name, which git chooses.
		set        map[string]string
		wantRemote string
		wantSource string
	}{
		"remote.pushDefault beats the origin convention": {
			set:        map[string]string{"remote.pushDefault": "fork"},
			wantRemote: "fork", wantSource: "remote.pushDefault",
		},
		"branch.<name>.pushRemote beats remote.pushDefault": {
			set: map[string]string{
				"branch.<branch>.pushRemote": "fork",
				"remote.pushDefault":         "origin",
			},
			wantRemote: "fork", wantSource: "branch.<branch>.pushRemote",
		},
		"remote.pushDefault beats branch.<name>.remote": {
			set: map[string]string{
				"remote.pushDefault":     "fork",
				"branch.<branch>.remote": "origin",
			},
			wantRemote: "fork", wantSource: "remote.pushDefault",
		},
		"branch.<name>.remote is the last of the three": {
			set:        map[string]string{"branch.<branch>.remote": "fork"},
			wantRemote: "fork", wantSource: "branch.<branch>.remote",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := newCheckout(t)
			expand := func(s string) string { return strings.ReplaceAll(s, "<branch>", c.branch) }
			for key, value := range tc.set {
				git(t, c.dir, "config", expand(key), value)
			}

			push, err := c.client.PushRemote(t.Context())
			require.NoError(t, err)
			assert.Equal(t,
				PushRemote{Name: tc.wantRemote, Source: expand(tc.wantSource)}, push)

			// And git agrees: the commit lands in the repository named above.
			commit := c.commitAndPush(t, "push routed by "+expand(tc.wantSource))
			assert.True(t, c.received(t, c.fork, commit), "git pushed to fork")
			assert.False(t, c.received(t, c.origin, commit), "git did not push to origin")
		})
	}
}

// The claim the push routing was originally built on — that these keys name a
// remote — is wrong: git-push(1) takes "either a URL or the name of a remote",
// and a bare `git push` follows a URL there to a repository no remote describes.
// Refusing that route as a broken remote name would refuse a checkout that
// deploys perfectly well, and falling back to origin would bind the repository
// this checkout demonstrably does not push to.
//
// The route is real git; the destination cannot be, since a test may not push to
// github.com. So this half proves a URL in the config is a working push route
// and that the selection refuses to answer "origin" for it, and
// TestSelectRemoteFollowsAURLInThePushConfiguration proves the GitHub URLs that
// reach the same code are followed to the repository they name.
func TestRealGitPushesToAURLInThePushConfiguration(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "config", "remote.pushDefault", c.unnamed)

	push, err := c.client.PushRemote(t.Context())
	require.NoError(t, err)
	assert.Equal(t, c.unnamed, push.Name)
	assert.Equal(t, "remote.pushDefault", push.Source)

	commit := c.commitAndPush(t, "to a repository no remote names")
	assert.True(t, c.received(t, c.unnamed, commit), "git pushed to the configured URL")
	assert.False(t, c.received(t, c.origin, commit), "git did not push to origin")

	// So the selection must not answer "origin". A local path is not a GitHub
	// repository, so the honest answer here is a refusal.
	remotes, err := c.client.Remotes(t.Context())
	require.NoError(t, err)
	_, err = SelectRemote(remotes, "", push)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote.pushDefault")
	assert.NotContains(t, err.Error(), "origin")
}

// An unset key is an answer, not a failure, and git says so with exit status 1
// specifically. A fake returning a plain error let this pass while the code
// could not tell an unset key from a repository it failed to read.
func TestRealGitReportsAnUnsetConfigKeyWithExitStatusOne(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)

	value, err := c.client.configValue(t.Context(), "remote.pushDefault")
	require.NoError(t, err)
	assert.Empty(t, value)

	// The exit status itself, since that is what the code branches on.
	err = gitCommand(t.Context(), c.dir, "git", "config", "--get", "remote.pushDefault").Run()
	require.Error(t, err)
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, 1, exitErr.ExitCode())
	assert.True(t, isUnsetConfigKey(err))

	push, err := c.client.PushRemote(t.Context())
	require.NoError(t, err)
	assert.Equal(t, PushRemote{}, push)
}

// A malformed config is a real failure, and reading it as "not set" would let
// this command quietly disagree with the git the user runs.
func TestRealGitReportsABrokenConfigAsAFailure(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	require.NoError(t, os.WriteFile(filepath.Join(c.dir, ".git", "config"),
		[]byte("[remote \"origin\"\n\tbroken\n"), 0o600))

	_, err := c.client.configValue(t.Context(), "remote.pushDefault")
	require.ErrorIs(t, err, ErrGitUnavailable)

	err = gitCommand(t.Context(), c.dir, "git", "config", "--get", "remote.pushDefault").Run()
	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.NotEqual(t, 1, exitErr.ExitCode(), "a real failure must not look like an unset key")
	assert.False(t, isUnsetConfigKey(err))
}

// On a detached HEAD git prints nothing and succeeds, so there is no branch to
// key branch.<name>.* off — not a failure to report.
func TestRealGitPrintsNoBranchOnADetachedHead(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "first")
	git(t, c.dir, "checkout", "--quiet", "--detach", "HEAD")

	branch, err := c.client.currentBranch(t.Context())
	require.NoError(t, err)
	assert.Empty(t, branch)

	// remote.pushDefault is still consulted; only the branch-scoped keys drop out.
	git(t, c.dir, "config", "remote.pushDefault", "fork")
	push, err := c.client.PushRemote(t.Context())
	require.NoError(t, err)
	assert.Equal(t, PushRemote{Name: "fork", Source: "remote.pushDefault"}, push)
}

// git refuses a colon in a remote name, which is what makes a colon a safe test
// for "this is a URL, not a name" — and it accepts "@", which is what makes "@"
// an unsafe one.
func TestRealGitRemoteNameRules(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)

	err := gitCommand(t.Context(), c.dir, "git", "remote", "add", "we:ird", c.unnamed).Run()
	require.Error(t, err, "git rejects a colon in a remote name")
	assert.True(t, looksLikeURL("we:ird"))

	git(t, c.dir, "remote", "add", "we@ird", c.unnamed)
	assert.False(t, looksLikeURL("we@ird"), "a name git accepts must not be read as a URL")
}
