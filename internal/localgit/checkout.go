package localgit

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// ErrNoCheckout indicates git could not treat this directory as a work tree.
// Not a repository at all is the ordinary cause; a repository git refuses to
// read is the other, and the two are told apart by git's own message rather
// than guessed at, so it is always appended.
var ErrNoCheckout = errors.New("this directory is not a Git work tree")

// Checkout is the local state a repository-creating command needs before it
// creates anything: which branch a push would carry, and whether there is
// anything to carry.
type Checkout struct {
	// Branch is the branch HEAD is on, empty on a detached HEAD. On a repository
	// with no commits git still reports the unborn branch it would create, which
	// is exactly the name the first push will use.
	Branch string
	// HasCommits reports whether HEAD resolves to a commit. A push from a
	// repository with none has nothing to send, and the branch does not exist on
	// the remote afterwards.
	HasCommits bool
}

// Checkout reports the local repository state, failing with ErrNoCheckout when
// there is no work tree to read.
func (c Client) Checkout(ctx context.Context) (Checkout, error) {
	if err := c.requireWorkTree(ctx); err != nil {
		return Checkout{}, err
	}

	branch, err := c.currentBranch(ctx)
	if err != nil {
		return Checkout{}, err
	}
	commits, err := c.hasCommits(ctx)
	if err != nil {
		return Checkout{}, err
	}
	return Checkout{Branch: branch, HasCommits: commits}, nil
}

// requireWorkTree refuses a directory git will not push from.
//
// The answer is taken from the exit status rather than from the word git prints:
// outside a repository git exits non-zero and prints nothing on stdout, and its
// stderr is translated when the user has a localized git, so matching on the
// message would work in English and quietly stop working elsewhere. What git
// said is still reported, because "not a repository" and "dubious ownership"
// need different fixes and only git knows which one this is.
func (c Client) requireWorkTree(ctx context.Context) error {
	out, err := c.runner.Run(ctx, "git", "rev-parse", "--is-inside-work-tree")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrNoCheckout, gitFailure(err))
	}
	// A bare repository answers "false" and exits 0. It has no working tree to
	// commit or push from, so it is refused here rather than at the push.
	if strings.TrimSpace(string(out)) != "true" {
		return fmt.Errorf("%w: it is a bare repository", ErrNoCheckout)
	}
	return nil
}

// hasCommits reports whether HEAD resolves.
//
// An unborn HEAD is not a failure — it is the state of every repository between
// `git init` and the first commit — and git says so with exit status 1 and
// nothing else, the same convention configValue relies on for an unset key.
// Anything else is a real failure and is reported as one.
func (c Client) hasCommits(ctx context.Context) (bool, error) {
	if _, err := c.runner.Run(ctx, "git", "rev-parse", "--quiet", "--verify", "HEAD"); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, gitFailure(err)
	}
	return true, nil
}

// BranchExists reports whether name is a local branch. It answers for the
// branch a push would name, so a caller can refuse a branch git cannot push
// before anything irreversible happens.
func (c Client) BranchExists(ctx context.Context, name string) (bool, error) {
	// The full refs/heads/ path, not the bare name: `git rev-parse --verify foo`
	// also resolves a tag or a remote-tracking branch called foo, and neither is
	// something `git push origin foo` sends.
	if _, err := c.runner.Run(ctx, "git", "rev-parse", "--quiet", "--verify", "refs/heads/"+name); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, gitFailure(err)
	}
	return true, nil
}
