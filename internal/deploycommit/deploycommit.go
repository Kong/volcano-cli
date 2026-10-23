// Package deploycommit reports which commit a cloud deploy's source came from,
// so Volcano can tie the deployment to its repository and LavaFlow can open
// fix pull requests against the code that actually runs.
//
// It sends only the GitHub repository as owner/name, the commit ID, and
// whether the working tree had uncommitted changes: never a remote URL, a
// path, a branch, or a diff. When any part is missing or would not pass the
// server's check, it reports nothing at all. The server refuses a malformed
// commit with a 400 before anything deploys, so sending a doubtful one would
// fail the customer's deploy over a field they never asked for.
package deploycommit

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/localgit"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// OptOutEnv turns reporting off when set to "off".
const OptOutEnv = "VOLCANO_DEPLOY_COMMIT"

// gitTimeout bounds each git call, so a slow or hung git never holds up a
// deploy; a timeout reports nothing, like any other failure.
const gitTimeout = 10 * time.Second

// The server's own checks, so a value it would refuse is never sent.
var (
	shaPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// Detect returns the commit the source in dir came from, or nil when it
// cannot say for sure. GitHub Actions' GITHUB_REPOSITORY and GITHUB_SHA come
// first; otherwise git's origin remote and HEAD. Either way git reports
// whether the working tree is dirty. Local development never reports one.
func Detect(ctx context.Context, deps cliruntime.Deps, dir string) *api.DeployCommit {
	if deps.LocalMode || strings.EqualFold(strings.TrimSpace(os.Getenv(OptOutEnv)), "off") {
		return nil
	}
	git := gitRunner(deps)

	var repository, sha string
	if os.Getenv("GITHUB_REPOSITORY") != "" || os.Getenv("GITHUB_SHA") != "" {
		repository, sha = fromGitHubActions()
	} else {
		repository, sha = fromGit(ctx, git, dir)
	}
	if !valid(repository, sha) {
		return nil
	}

	status, err := run(ctx, git, dir, "status", "--porcelain")
	if err != nil {
		return nil
	}
	return &api.DeployCommit{Repository: repository, SHA: sha, Dirty: strings.TrimSpace(status) != ""}
}

// fromGitHubActions reads the workflow's repository and commit. On a
// pull_request event GITHUB_SHA is the merge commit GitHub made for the
// check, which is on no branch, so the server records it unverified: true,
// and nothing more is claimed. The server must be github.com, since GitHub
// Enterprise Server sets these too, for a repository Volcano cannot reach.
func fromGitHubActions() (repository, sha string) {
	server := strings.TrimRight(strings.TrimSpace(os.Getenv("GITHUB_SERVER_URL")), "/")
	if server != "" && !strings.EqualFold(server, "https://github.com") {
		return "", ""
	}
	return strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY")), strings.ToLower(strings.TrimSpace(os.Getenv("GITHUB_SHA")))
}

// fromGit reads origin's GitHub repository and the checked-out commit. The
// remote URL is reduced to owner/name, dropping any credentials in it.
func fromGit(ctx context.Context, git cliruntime.CommandRunner, dir string) (repository, sha string) {
	remote, err := run(ctx, git, dir, "remote", "get-url", "origin")
	if err != nil {
		return "", ""
	}
	parsed, err := localgit.ParseGitHubRepository(remote)
	if err != nil {
		return "", ""
	}
	head, err := run(ctx, git, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", ""
	}
	return parsed.FullName(), strings.ToLower(strings.TrimSpace(head))
}

// valid applies the server's checks: a GitHub owner/name and a full SHA-1
// commit ID. A SHA-256 repository's 64-character IDs do not pass.
func valid(repository, sha string) bool {
	owner, name, ok := strings.Cut(repository, "/")
	return ok && ownerPattern.MatchString(owner) && namePattern.MatchString(name) &&
		name != "." && name != ".." && shaPattern.MatchString(sha)
}

func run(ctx context.Context, git cliruntime.CommandRunner, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := git.Run(ctx, "git", append([]string{"-C", dir}, args...)...)
	return string(out), err
}

func gitRunner(deps cliruntime.Deps) cliruntime.CommandRunner {
	if deps.GitCommandRunner != nil {
		return deps.GitCommandRunner
	}
	return cliruntime.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // the command is git; its arguments are constants and the deploy's own directory
	})
}
