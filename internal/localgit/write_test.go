package localgit

// These run the real git binary, for the reason the preamble of realgit_test.go
// gives: the writes here are asserted by what git does with them, not by what a
// fake was told they mean. A fake runner cannot show that `git push
// --set-upstream -- origin main` sets an upstream, or that refs/heads/ is what
// keeps a tag from passing for a branch.

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckoutReportsAnUnbornBranchBeforeTheFirstCommit(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)

	state, err := c.client.Checkout(t.Context())
	require.NoError(t, err)
	// The unborn branch is reported, and it is the name the first push will
	// create. This is the whole reason the CLI can name a production branch
	// instead of leaving the platform to predict one.
	assert.Equal(t, c.branch, state.Branch)
	assert.False(t, state.HasCommits, "a repository with no commits has none to push")

	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "first")

	state, err = c.client.Checkout(t.Context())
	require.NoError(t, err)
	assert.Equal(t, c.branch, state.Branch)
	assert.True(t, state.HasCommits)
}

func TestCheckoutRefusesADirectoryThatIsNotARepository(t *testing.T) {
	t.Parallel()
	client := realGitClient(t, t.TempDir())

	_, err := client.Checkout(t.Context())
	require.ErrorIs(t, err, ErrNoCheckout)
	// git's own message is kept: "not a repository" and a config git refuses to
	// read exit the same way, and only one of them is fixed by `git init`.
	assert.ErrorIs(t, err, ErrGitUnavailable)
}

func TestCheckoutRefusesABareRepository(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)

	_, err := realGitClient(t, c.origin).Checkout(t.Context())
	require.ErrorIs(t, err, ErrNoCheckout)
	// A bare repository answers the work-tree question with "false" and exit 0,
	// so it is not a git failure — it is a directory with nothing to push from.
	assert.Contains(t, err.Error(), "bare repository")
	assert.NotErrorIs(t, err, ErrGitUnavailable)
}

func TestCheckoutOnADetachedHeadReportsNoBranch(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "first")
	git(t, c.dir, "checkout", "--quiet", "--detach")

	state, err := c.client.Checkout(t.Context())
	require.NoError(t, err)
	assert.Empty(t, state.Branch, "a detached HEAD is on no branch")
	assert.True(t, state.HasCommits)
}

func TestBranchExistsAnswersForLocalBranchesOnly(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "first")
	git(t, c.dir, "branch", "release")
	// A tag resolves under the bare name git rev-parse would take, but `git push
	// origin release-tag` does not push a branch of that name. Answering true for
	// it would let the CLI promise a push it cannot make.
	git(t, c.dir, "tag", "release-tag")

	exists, err := c.client.BranchExists(t.Context(), "release")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = c.client.BranchExists(t.Context(), "release-tag")
	require.NoError(t, err)
	assert.False(t, exists, "a tag is not a branch")

	exists, err = c.client.BranchExists(t.Context(), "nothing-here")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestAddRemoteRecordsTheURLVerbatim(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	url := HTTPSRemoteURL("octo/storefront")

	require.NoError(t, c.client.AddRemote(t.Context(), "volcano", url))
	assert.Equal(t, url, git(t, c.dir, "remote", "get-url", "volcano"))

	config, err := os.ReadFile(filepath.Join(c.dir, ".git", "config"))
	require.NoError(t, err)
	// The invariant this whole flow rests on: nothing Volcano writes into a Git
	// config carries a credential.
	assert.NotContains(t, string(config), "@github.com")
}

func TestAddRemoteRefusesAnExistingName(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	before := git(t, c.dir, "remote", "get-url", "origin")

	err := c.client.AddRemote(t.Context(), "origin", HTTPSRemoteURL("octo/elsewhere"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `could not add the "origin" remote`)
	// Taking the name over would silently redirect where this checkout pushes.
	assert.Equal(t, before, git(t, c.dir, "remote", "get-url", "origin"))
}

func TestPushSendsTheBranchAndSetsItsUpstream(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "first")
	require.NoError(t, c.client.AddRemote(t.Context(), "volcano", c.unnamed))

	var out logWriter
	require.NoError(t, c.client.Push(t.Context(), &out, "volcano", c.branch))

	commit := git(t, c.dir, "rev-parse", "HEAD")
	assert.Equal(t, commit, c.headOf(t, c.unnamed), "the branch reached the repository")
	// The upstream is what makes the next bare `git push` work, which is the
	// whole point of setting it on the first one.
	assert.Equal(t, "volcano", git(t, c.dir, "config", "branch."+c.branch+".remote"))
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "second")
	git(t, c.dir, "push", "--quiet")
	assert.Equal(t, git(t, c.dir, "rev-parse", "HEAD"), c.headOf(t, c.unnamed))
}

func TestPushReportsTheRemoteItFailedOn(t *testing.T) {
	t.Parallel()
	c := newCheckout(t)
	git(t, c.dir, "commit", "--quiet", "--allow-empty", "-m", "first")
	require.NoError(t, c.client.AddRemote(t.Context(), "volcano", filepath.Join(c.root, "not-a-repository")))

	var out logWriter
	err := c.client.Push(t.Context(), &out, "volcano", c.branch)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `git push to "volcano" failed`)
	// git's own diagnosis reaches the user rather than being swallowed into the
	// wrapper: it is the only thing that says which of many push failures this is.
	assert.NotEmpty(t, out.String())
}

// The claim ValidRemoteName rests on: `git check-ref-format --allow-onelevel`
// answers the same question `git remote add` asks. Asserted against both, so a
// git that ever disagreed would fail here rather than after a repository has
// been created for a remote name that cannot be recorded.
func TestValidRemoteNameAgreesWithGitRemoteAdd(t *testing.T) {
	t.Parallel()
	// The interesting half is the accepted column: shell metacharacters are legal
	// remote names, which is why the printed commands are quoted.
	for _, name := range []string{"origin", "volcano", "a/b", "a;b", "a$b", ".a", "a..b", "a~b", "a.lock", `a\b`} {
		c := newCheckout(t)
		// The fixture's own remotes are removed first: "origin" already existing
		// would fail the add for a reason that has nothing to do with the name.
		git(t, c.dir, "remote", "remove", "origin")
		git(t, c.dir, "remote", "remove", "fork")

		valid, err := c.client.ValidRemoteName(t.Context(), name)
		require.NoErrorf(t, err, "checking %q", name)

		err = c.client.AddRemote(t.Context(), name, HTTPSRemoteURL("octo/storefront"))
		assert.Equalf(t, err == nil, valid, "git remote add and check-ref-format disagree on %q", name)
	}
}

func TestRemoteURLsCarryNoCredential(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "https://github.com/octo/storefront.git", HTTPSRemoteURL("octo/storefront"))
	assert.Equal(t, "git@github.com:octo/storefront.git", SSHRemoteURL("octo/storefront"))
	// A full name that already ends in .git must not produce "storefront.git.git",
	// which is a different repository name on GitHub.
	assert.Equal(t, "https://github.com/octo/storefront.git", HTTPSRemoteURL("octo/storefront.git"))
	assert.Equal(t, "git@github.com:octo/storefront.git", SSHRemoteURL("octo/storefront.git"))
}

// logWriter collects what git wrote, standing in for the terminal the real
// command hands over.
type logWriter struct{ written []byte }

func (w *logWriter) Write(p []byte) (int, error) {
	w.written = append(w.written, p...)
	return len(p), nil
}

func (w *logWriter) String() string { return string(w.written) }

var _ io.Writer = (*logWriter)(nil)
