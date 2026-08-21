package localgit

import (
	"context"
	"errors"
	"os/exec"
)

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
