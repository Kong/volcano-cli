package localgit

import (
	"context"
	"fmt"
	"io"
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

// AddRemote records url under name.
//
// It fails when name is already taken rather than replacing it, which is git's
// own behavior and the safe one: the existing remote is where this checkout
// already pushes, and overwriting it would silently redirect that.
func (c Client) AddRemote(ctx context.Context, name, url string) error {
	if _, err := c.runner.Run(ctx, "git", "remote", "add", "--", name, url); err != nil {
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
