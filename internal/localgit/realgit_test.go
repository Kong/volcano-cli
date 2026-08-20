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
	root    string
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
	c := checkout{root: root}
	origin, fork, unnamed := c.bare(t, "origin.git"), c.bare(t, "fork.git"), c.bare(t, "unnamed.git")

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

	c.dir, c.branch = work, git(t, work, "branch", "--show-current")
	c.origin, c.fork, c.unnamed = origin, fork, unnamed
	c.client = realGitClient(t, work)
	return c
}

// bare creates a bare repository at relative path under the checkout's root and
// returns its absolute path, so a test can use it as a rewrite target.
func (c checkout) bare(t *testing.T, relative string) string {
	t.Helper()
	path := filepath.Join(c.root, relative)
	require.NoError(t, os.MkdirAll(path, 0o755))
	git(t, path, "init", "--quiet", "--bare")
	return path
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
	return c.headOf(t, bare) == commit
}

// headOf reports the checkout's branch in a bare repository, or "" when the
// branch is not there — meaning nothing was pushed to it.
func (c checkout) headOf(t *testing.T, bare string) string {
	t.Helper()
	out, err := gitCommand(t.Context(), bare, "git", "rev-parse", c.branch).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// assertPushGoesTo is the claim the rewriting has to satisfy: the URL resolved
// out of the configuration is the repository a real `git push` updates.
func (c checkout) assertPushGoesTo(t *testing.T, want string) {
	t.Helper()
	push, err := c.client.PushRemote(t.Context())
	require.NoError(t, err)
	assert.Equal(t, want, push.RewrittenURL, "the URL resolved from the configuration")

	commit := c.commitAndPush(t, "rewritten push")
	assert.Equal(t, commit, c.headOf(t, want), "the repository git actually pushed to")
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

// A checkout with no remotes at all can still have a push route, so an empty
// `git remote -v` is not on its own a refusal. Reading the remote list first and
// failing on it made the URL route unreachable exactly where it is the only
// route there is.
func TestRealGitPushesWithNoRemotesConfigured(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "remote", "remove", "origin")
	git(t, c.dir, "remote", "remove", "fork")
	git(t, c.dir, "config", "remote.pushDefault", c.unnamed)

	remotes, err := c.client.Remotes(t.Context())
	require.NoError(t, err, "an empty remote list is not Remotes' verdict to give")
	assert.Empty(t, remotes)

	push, err := c.client.PushRemote(t.Context())
	require.NoError(t, err)
	assert.Equal(t, c.unnamed, push.Name)

	commit := c.commitAndPush(t, "pushed with no remotes configured")
	assert.True(t, c.received(t, c.unnamed, commit), "git pushed with no remotes configured")

	// So the selection must consult the push route before the empty list. A
	// local path is not a GitHub repository, so the refusal names that instead.
	_, err = SelectRemote(remotes, "", push)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrNoRemotes)
	assert.Contains(t, err.Error(), "remote.pushDefault")
}

// git rewrites a push destination before using it, and no git command resolves
// that for a bare URL — `git remote get-url --push` refuses a remote that exists
// only in -c config ("No such remote"), and `git ls-remote --get-url` applies
// only the fetch-side rule. So the rules are implemented by hand, which means
// each one has to be asserted against where a real push landed rather than
// against the documentation it was read from.
//
// The configured URL is unreachable on purpose: if a rule failed to apply, the
// push would try to reach gh.test rather than quietly land somewhere plausible.
func TestRealGitPushURLRewritingAgreesWithWhereGitPushes(t *testing.T) {
	t.Parallel()
	const configured = "https://gh.test/octo/app.git"

	t.Run("pushInsteadOf rewrites the whole URL", func(t *testing.T) {
		t.Parallel()
		c := newCheckout(t)
		target := c.bare(t, "rw.git")
		git(t, c.dir, "config", "remote.pushDefault", configured)
		git(t, c.dir, "config", "url."+target+".pushInsteadOf", configured)

		c.assertPushGoesTo(t, target)
	})

	// insteadOf is a fetch-side rule, but a push with no pushInsteadOf follows it.
	t.Run("insteadOf applies when no pushInsteadOf matches", func(t *testing.T) {
		t.Parallel()
		c := newCheckout(t)
		target := c.bare(t, "rw.git")
		git(t, c.dir, "config", "remote.pushDefault", configured)
		git(t, c.dir, "config", "url."+target+".insteadOf", configured)

		c.assertPushGoesTo(t, target)
	})

	// A pushInsteadOf that does not match must not suppress one that does: the
	// question is whether a push rule matched this URL, not whether any exists.
	t.Run("a non-matching pushInsteadOf does not shadow insteadOf", func(t *testing.T) {
		t.Parallel()
		c := newCheckout(t)
		target, unused := c.bare(t, "rw.git"), c.bare(t, "unused.git")
		git(t, c.dir, "config", "remote.pushDefault", configured)
		git(t, c.dir, "config", "url."+unused+".pushInsteadOf", "https://elsewhere.test/")
		git(t, c.dir, "config", "url."+target+".insteadOf", configured)

		c.assertPushGoesTo(t, target)
	})

	// And when both match, the push rule wins — however much shorter it is.
	t.Run("pushInsteadOf beats insteadOf", func(t *testing.T) {
		t.Parallel()
		c := newCheckout(t)
		pushTarget, fetchTarget := c.bare(t, "push.git"), c.bare(t, "fetch.git")
		git(t, c.dir, "config", "remote.pushDefault", configured)
		git(t, c.dir, "config", "url."+fetchTarget+".insteadOf", configured)
		git(t, c.dir, "config", "url."+pushTarget+".pushInsteadOf", configured)

		c.assertPushGoesTo(t, pushTarget)
		assert.Empty(t, c.headOf(t, fetchTarget), "the fetch-side rule must not win")
	})

	// The base is a prefix and the remainder is appended, so a directory base
	// rewrites a family of URLs at once.
	t.Run("the remainder is appended to the base", func(t *testing.T) {
		t.Parallel()
		c := newCheckout(t)
		c.bare(t, "nest/app.git")
		git(t, c.dir, "config", "remote.pushDefault", configured)
		git(t, c.dir, "config", "url."+filepath.Join(c.root, "nest")+"/.pushInsteadOf",
			"https://gh.test/octo/")

		c.assertPushGoesTo(t, filepath.Join(c.root, "nest", "app.git"))
	})

	// Two rules match; the longer prefix decides. Picking either arbitrarily
	// would bind a repository the push does not reach.
	t.Run("the longest matching prefix wins", func(t *testing.T) {
		t.Parallel()
		c := newCheckout(t)
		c.bare(t, "deep/app.git")
		c.bare(t, "shallow/app.git")
		git(t, c.dir, "config", "remote.pushDefault", configured)
		git(t, c.dir, "config", "url."+filepath.Join(c.root, "shallow")+"/.pushInsteadOf",
			"https://gh.test/")
		git(t, c.dir, "config", "url."+filepath.Join(c.root, "deep")+"/.pushInsteadOf",
			"https://gh.test/octo/")

		c.assertPushGoesTo(t, filepath.Join(c.root, "deep", "app.git"))
		assert.Empty(t, c.headOf(t, filepath.Join(c.root, "shallow", "app.git")),
			"the shorter prefix must not win")
	})

	// No rules at all is the ordinary case, and reports no rewrite rather than
	// echoing the value back as though one applied.
	t.Run("no rules means no rewrite", func(t *testing.T) {
		t.Parallel()
		c := newCheckout(t)
		git(t, c.dir, "config", "remote.pushDefault", c.unnamed)

		push, err := c.client.PushRemote(t.Context())
		require.NoError(t, err)
		assert.Empty(t, push.RewrittenURL)
		assert.Equal(t, c.unnamed, push.Name)
	})
}

// Two rules sharing a prefix, as `git config --add` produces: git follows the
// first. Keying the rules by prefix and assigning over the earlier one resolved
// the second while the push went to the first.
func TestRealGitFollowsTheFirstOfTwoRulesSharingAPrefix(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	first, second := c.bare(t, "first.git"), c.bare(t, "second.git")
	const configured = "https://gh.test/octo/app.git"
	git(t, c.dir, "config", "remote.pushDefault", configured)
	git(t, c.dir, "config", "--add", "url."+first+".pushInsteadOf", configured)
	git(t, c.dir, "config", "--add", "url."+second+".pushInsteadOf", configured)

	c.assertPushGoesTo(t, first)
	assert.Empty(t, c.headOf(t, second), "the later rule must not win")
}

// The fetch-side rules are read the same way and need the same first-wins
// handling — measured separately rather than assumed symmetric.
func TestRealGitFollowsTheFirstOfTwoFetchRulesSharingAPrefix(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	first, second := c.bare(t, "first.git"), c.bare(t, "second.git")
	const configured = "https://gh.test/octo/app.git"
	git(t, c.dir, "config", "remote.pushDefault", configured)
	git(t, c.dir, "config", "--add", "url."+first+".insteadOf", configured)
	git(t, c.dir, "config", "--add", "url."+second+".insteadOf", configured)

	c.assertPushGoesTo(t, first)
	assert.Empty(t, c.headOf(t, second), "the later rule must not win")
}

// git prints the url.<base> subsection verbatim, and a base is often a local
// path, which may contain a space. Splitting `--get-regexp` output on the first
// space truncated the key, no suffix matched, and the rule was dropped without a
// word — leaving the binding on the repository the setting spells while every
// push went to the rewritten one. That is the silent wrong bind this resolution
// exists to prevent, so the space has to be in the base, not just the value.
func TestRealGitResolvesARewriteBaseContainingASpace(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	target := c.bare(t, "my mirrors/app.git")
	const configured = "https://gh.test/octo/app.git"
	git(t, c.dir, "config", "remote.pushDefault", configured)
	git(t, c.dir, "config", "url."+filepath.Join(c.root, "my mirrors")+"/.pushInsteadOf",
		"https://gh.test/octo/")

	c.assertPushGoesTo(t, target)
}

// A space in the value was always handled; kept alongside so the pair documents
// which half was broken.
func TestRealGitResolvesARewritePrefixContainingASpace(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	target := c.bare(t, "mirror.git")
	const configured = "https://gh.test/o c/app.git"
	git(t, c.dir, "config", "remote.pushDefault", configured)
	git(t, c.dir, "config", "url."+target+".pushInsteadOf", configured)

	c.assertPushGoesTo(t, target)
}

// git matches an empty insteadOf prefix against every URL, since every string
// starts with "". Skipping the rule reported no rewrite for a configuration under
// which git cannot push at all, so the CLI would bind a repository for a checkout
// with no working push route.
func TestRealGitAppliesAnEmptyRewritePrefixToEveryURL(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "config", "remote.pushDefault", c.unnamed)
	git(t, c.dir, "config", "url."+filepath.Join(c.root, "nowhere")+"/.insteadOf", "")

	push, err := c.client.PushRemote(t.Context())
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(c.root, "nowhere")+"/"+c.unnamed, push.RewrittenURL,
		"the empty prefix matches, so the base is prepended whole")

	// git agrees: it refuses, rather than pushing to the unrewritten value.
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "empty prefix")
	require.Error(t, gitCommand(t.Context(), c.dir, "git", "push", "--quiet").Run())
	assert.Empty(t, c.headOf(t, c.unnamed), "git did not fall back to the configured value")
}

// git lists a remote that has no URL — one configured with only a fetch refspec,
// or with an empty url, which git discards rather than stores. Dropping that line
// made a named lookup deny a remote git still lists, and silently moved the
// selection to another remote.
func TestRealGitListsARemoteWithNoURL(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "config", "remote.ghost.fetch", "+refs/heads/*:refs/remotes/ghost/*")

	remotes, err := c.client.Remotes(t.Context())
	require.NoError(t, err)

	var ghost *Remote
	for i := range remotes {
		if remotes[i].Name == "ghost" {
			ghost = &remotes[i]
		}
	}
	require.NotNil(t, ghost, "git lists it, so it must not be dropped")
	assert.Empty(t, ghost.PushURLs)
	assert.Empty(t, ghost.FetchURL)

	// And naming it reports what is wrong with it, rather than denying it exists.
	_, err = SelectRemote(remotes, "ghost", PushRemote{})
	require.NoError(t, err, "the remote is found")
	_, err = ghost.PushTarget()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has nothing to push to")
}

// git's config syntax admits a value ending in a newline, and `git config --get`
// then emits that newline plus its own terminator. Removing both read the value
// as the valid remote "origin" and bound that repository, while git refuses the
// value outright.
func TestRealGitRefusesAValueEndingInANewline(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	appendGitConfig(t, c.dir, "[remote]\n\tpushdefault = \"origin\\n\"\n")

	value, set, err := c.client.configValue(t.Context(), "remote.pushDefault")
	require.NoError(t, err)
	require.True(t, set)
	assert.Equal(t, "origin\n", value, "only git's own terminator comes off")

	_, err = c.client.PushRemote(t.Context())
	require.Error(t, err, "the value keeps its newline, so it is refused as padded")

	// git agrees: it does not read this as the origin remote.
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "newline route")
	require.Error(t, gitCommand(t.Context(), c.dir, "git", "push", "--quiet").Run())
	assert.Empty(t, c.headOf(t, c.origin), "git did not fall back to the origin remote")
}

func appendGitConfig(t *testing.T, dir, content string) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(dir, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = file.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, file.Close())
}

// A matching pushInsteadOf wins even when it rewrites the URL to itself: git
// does not fall through to insteadOf. Treating an identity rewrite as "no
// rewrite" reached the fetch rule and resolved a different repository — a guard
// that looked like a harmless shortcut until a real push disagreed with it.
//
// The configured value is a path that does not exist, so honouring the identity
// rule fails the push and falling through would have succeeded. No network.
func TestRealGitHonoursAnIdentityPushInsteadOf(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	configured := filepath.Join(c.root, "does-not-exist.git")
	fetchTarget := c.bare(t, "fetch-target.git")
	git(t, c.dir, "config", "remote.pushDefault", configured)
	git(t, c.dir, "config", "url."+configured+".pushInsteadOf", configured)
	git(t, c.dir, "config", "url."+fetchTarget+".insteadOf", configured)

	push, err := c.client.PushRemote(t.Context())
	require.NoError(t, err)
	assert.Equal(t, configured, push.RewrittenURL, "the identity push rule wins")
	assert.NotEqual(t, fetchTarget, push.RewrittenURL, "the fetch rule must not be reached")

	// git agrees: it tries the path that does not exist, not the repository the
	// fetch rule names.
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "identity rewrite")
	require.Error(t, gitCommand(t.Context(), c.dir, "git", "push", "--quiet").Run())
	assert.Empty(t, c.headOf(t, fetchTarget), "git did not fall back to the insteadOf rule")
}

// Why the rewrite is applied only to a value used as a URL: for a named remote
// git has already done it, and `git remote -v` prints the rewritten URL on the
// (push) line. Rewriting again here would rewrite an already-rewritten URL.
func TestRealGitRemoteVAlreadyShowsTheRewrittenPushURL(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	target := c.bare(t, "rw.git")
	const configured = "https://gh.test/octo/app.git"
	git(t, c.dir, "remote", "set-url", "fork", configured)
	git(t, c.dir, "config", "url."+target+".pushInsteadOf", configured)

	remotes, err := c.client.Remotes(t.Context())
	require.NoError(t, err)
	var fork Remote
	for _, remote := range remotes {
		if remote.Name == "fork" {
			fork = remote
		}
	}
	pushURL, err := fork.PushTarget()
	require.NoError(t, err)
	assert.Equal(t, target, pushURL, "git remote -v resolves the push rewrite itself")
	assert.Equal(t, configured, fork.FetchURL, "and leaves the fetch URL alone")
}

// A routing key that is set decides the route even when its value cannot work:
// git does not fall through to the next key, it fails. Treating one of these as
// "not configured" bound whatever the origin convention picked, for a checkout
// that cannot push at all.
func TestRealGitDoesNotFallThroughFromAnUnusablePushRoute(t *testing.T) {
	t.Parallel()
	for name, value := range map[string]string{
		"an empty value":       "",
		"a whitespace value":   "   ",
		"a padded remote name": " fork ",
	} {
		t.Run(name+" is refused rather than skipped", func(t *testing.T) {
			t.Parallel()
			c := newCheckout(t)
			git(t, c.dir, "config", "branch."+c.branch+".pushRemote", value)
			// A working route behind it, which git does not reach and neither
			// may this.
			git(t, c.dir, "config", "remote.pushDefault", "fork")

			_, err := c.client.PushRemote(t.Context())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "branch."+c.branch+".pushRemote")

			// git agrees: the push fails, and nothing reaches fork.
			git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "unusable route")
			require.Error(t,
				gitCommand(t.Context(), c.dir, "git", "push", "--quiet").Run(),
				"git must refuse this configuration too")
			assert.Empty(t, c.headOf(t, c.fork), "git did not fall through to remote.pushDefault")
		})
	}
}

// A padded value must not be trimmed into something that works. git uses it
// verbatim, so " <url> " is a repository that does not exist — and trimming it
// bound a repository no push reaches.
func TestRealGitRefusesAPaddedPushURL(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "config", "remote.pushDefault", " https://github.com/octo/app.git ")

	_, err := c.client.PushRemote(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "verbatim")

	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "padded route")
	require.Error(t, gitCommand(t.Context(), c.dir, "git", "push", "--quiet").Run(),
		"git refuses the padded value too")
}

// An unset key is an answer, not a failure, and git says so with exit status 1
// specifically. A fake returning a plain error let this pass while the code
// could not tell an unset key from a repository it failed to read.
func TestRealGitReportsAnUnsetConfigKeyWithExitStatusOne(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)

	value, set, err := c.client.configValue(t.Context(), "remote.pushDefault")
	require.NoError(t, err)
	assert.Empty(t, value)
	assert.False(t, set, "an unset key reports itself as unset, not as empty")

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

	_, _, err := c.client.configValue(t.Context(), "remote.pushDefault")
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
