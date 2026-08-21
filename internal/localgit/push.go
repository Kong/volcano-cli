package localgit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
)

// GitHubHost is the only host these commands bind repositories on.
const gitHubHost = "github.com"

// HTTPSRemoteURL returns the https remote URL for a github.com repository.
//
// https rather than ssh because it is the form that works with no further setup
// — a credential helper, or the token one already holds — while ssh needs a key
// on the account. It is also the form git rewrites: a user who prefers ssh
// almost always has url."git@github.com:".insteadOf configured, and git applies
// that to this URL on every push, so recording https does not force https on
// them. RemoteURL carries no credential either way.
func HTTPSRemoteURL(fullName string) string {
	return "https://" + gitHubHost + "/" + strings.TrimSuffix(fullName, ".git") + ".git"
}

// SSHRemoteURL returns the ssh remote URL for a github.com repository, for a
// caller who asked for ssh explicitly.
func SSHRemoteURL(fullName string) string {
	return "git@" + gitHubHost + ":" + strings.TrimSuffix(fullName, ".git") + ".git"
}

// ValidRemoteName reports whether git would accept name as a remote name.
//
// Asked of git rather than reimplemented here. `git remote add` applies git's
// ref-format rules, which refuse a good deal more than whitespace — a leading
// dot, "..", "~", a ".lock" suffix — while allowing plenty that looks unlikely,
// including ";" and "$". `git check-ref-format --allow-onelevel` is that same
// check, and agrees with `git remote add` on every name tried. Guessing instead
// would either refuse names git accepts or accept names it refuses, and the
// second one costs a repository: the refusal would land after the create.
//
// A name starting with "-" has to be refused by the caller. check-ref-format
// takes no "--" separator, so git would read it as an option rather than answer
// the question.
func (c Client) ValidRemoteName(ctx context.Context, name string) (bool, error) {
	if _, err := c.runner.Run(ctx, "git", "check-ref-format", "--allow-onelevel", name); err != nil {
		var exitErr *exec.ExitError
		// git says "no" with exit status 1 and nothing else, the same convention
		// the unset-config and unborn-HEAD reads rely on.
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, gitFailure(err)
	}
	return true, nil
}

// HasRemoteBranches reports whether a repository already has branches, which is
// what tells an empty repository from one with history.
//
// Run through the terminal runner rather than the capturing one, because a
// private repository needs credentials and a credential helper with no terminal
// simply fails. The output is captured anyway — the writer is ours, not the
// user's — so the answer can be read without the user seeing a ref listing they
// did not ask for.
func (c Client) HasRemoteBranches(ctx context.Context, repoURL string) (bool, error) {
	var out bytes.Buffer
	if err := c.terminal.Run(ctx, &out, "git", "ls-remote", "--heads", "--", repoURL); err != nil {
		return false, fmt.Errorf("could not read %s: %w", Redact(repoURL), err)
	}
	// Both streams land in the buffer, so match the ref lines rather than
	// treating any output as branches: git writes progress and credential notices
	// here too.
	for line := range strings.SplitSeq(out.String(), "\n") {
		if strings.Contains(line, "\trefs/heads/") {
			return true, nil
		}
	}
	return false, nil
}

// PushTo sends a local branch to a differently named branch on the remote,
// without setting an upstream.
//
// The upstream is left alone on purpose: this is the export-to-a-side-branch
// path, and pointing the local branch at it would make every later bare push go
// to the side branch rather than to wherever the user already pushes.
func (c Client) PushTo(ctx context.Context, out io.Writer, remote, local, remoteBranch string) error {
	refspec := local + ":refs/heads/" + remoteBranch
	if err := c.terminal.Run(ctx, out, "git", "push", "--", remote, refspec); err != nil {
		return fmt.Errorf("git push of %s to %q failed: %w", refspec, remote, err)
	}
	return nil
}

// CompareURL is where a pull request for branch against base is opened.
func CompareURL(fullName, base, branch string) string {
	return fmt.Sprintf("https://%s/%s/compare/%s...%s?expand=1",
		gitHubHost, strings.TrimSuffix(fullName, ".git"), escapeRef(base), escapeRef(branch))
}

// escapeRef makes a branch name safe in a URL path while leaving "/" alone.
//
// git refuses whitespace and "~^:?*[\" in a ref name but allows "#" and "%",
// either of which would cut the URL short or be read as an escape. A slash is
// escaped back because branch names routinely contain one and GitHub's compare
// URLs take it literally.
func escapeRef(ref string) string {
	return strings.ReplaceAll(url.PathEscape(ref), "%2F", "/")
}

// AddRemote records url under name.
//
// It fails when name is already taken rather than replacing it, which is git's
// own behavior and the safe one: the existing remote is where this checkout
// already pushes, and overwriting it would silently redirect that.
func (c Client) AddRemote(ctx context.Context, name, repoURL string) error {
	if _, err := c.runner.Run(ctx, "git", "remote", "add", "--", name, repoURL); err != nil {
		return fmt.Errorf("could not add the %q remote: %w", name, gitFailure(err))
	}
	return nil
}

// Push sends branch to remote and sets it as the branch's upstream, with the
// user's terminal attached so git can prompt for credentials and report
// progress.
//
// The upstream is set because the push is the first one: without it a later bare
// `git push` fails with "no upstream configured", which is a poor reward for
// having used this command. It writes only branch.<name>.remote and
// branch.<name>.merge — never a credential.
func (c Client) Push(ctx context.Context, out io.Writer, remote, branch string) error {
	// "--" separates the refspec from the options for a branch whose name could
	// read as one. The command layer refuses such a name before reaching here;
	// this keeps that from being the only thing standing between a branch name
	// and git's argument parser.
	if err := c.terminal.Run(ctx, out, "git", "push", "--set-upstream", "--", remote, branch); err != nil {
		return fmt.Errorf("git push to %q failed: %w", remote, err)
	}
	return nil
}
