package deploycommit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/localgit"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// Uploads records the commit each upload of one deploy run reported, so the
// run can say once whether LavaFlow can read the code it deployed. A nil
// *Uploads records nothing.
type Uploads struct {
	uploads []sentUpload
}

type sentUpload struct {
	resource string
	// dir is where the commit was read: a frontend's uploaded directory, or
	// the project directory function packaging reads from.
	dir      string
	source   *Source
	reported bool
	dirty    bool
}

// AddFrontend records a frontend upload of dir, the directory its archive
// holds, and the commit it reported.
func (u *Uploads) AddFrontend(name, dir string, source *Source, commit *api.DeployCommit) {
	if u == nil {
		return
	}
	u.uploads = append(u.uploads, sentUpload{
		resource: "frontend " + name, dir: dir, source: source,
		reported: commit != nil, dirty: commit != nil && commit.Dirty,
	})
}

// AddFunction records a function upload read from dir, the project
// directory, and the commit it reported. kind names the resource, such as
// "function" or "durable function".
func (u *Uploads) AddFunction(kind, name, dir string, source *Source, commit *api.FunctionCommit) {
	if u == nil {
		return
	}
	u.uploads = append(u.uploads, sentUpload{
		resource: kind + " " + name, dir: dir, source: source,
		reported: commit != nil, dirty: commit != nil && commit.Dirty,
	})
}

// Len is how many uploads u records.
func (u *Uploads) Len() int {
	if u == nil {
		return 0
	}
	return len(u.uploads)
}

// Target is where LavaFlow reads code: commits of the project's connected
// repository, as owner/name, that its production branch holds.
type Target struct {
	Repository string
	Branch     string
}

// ReasonKind is why LavaFlow can't read an upload's code.
type ReasonKind int

// The reasons. The first six mean the upload reported no commit at all.
const (
	// ReasonOptedOut: OptOutEnv turned reporting off.
	ReasonOptedOut ReasonKind = iota + 1
	// ReasonNoGit: git isn't installed.
	ReasonNoGit
	// ReasonNotCheckout: the directory isn't in a Git checkout.
	ReasonNotCheckout
	// ReasonNoCommit: the checkout has no commit yet.
	ReasonNoCommit
	// ReasonNotGitHub: the checkout's origin remote isn't a GitHub
	// repository.
	ReasonNotGitHub
	// ReasonUnread: git couldn't read the checkout's commit.
	ReasonUnread
	// ReasonUncommitted: the checkout has uncommitted changes, which Paths
	// lists.
	ReasonUncommitted
	// ReasonDiffers: the upload doesn't match commit SHA, though git shows
	// no uncommitted change under its directory.
	ReasonDiffers
	// ReasonOtherRepository: commit SHA is from Repository, not the target's.
	ReasonOtherRepository
	// ReasonOffBranch: the checkout's copy of the target's production branch
	// doesn't hold commit SHA.
	ReasonOffBranch
)

// Reason is why LavaFlow can't read an upload's code.
type Reason struct {
	Kind ReasonKind
	// Paths are the uncommitted paths under the upload's directory, from the
	// repository root, in git's order.
	Paths []string
	// SHA is the commit checked out.
	SHA string
	// Repository is the commit's repository as owner/name.
	Repository string
}

// Unreadable says which uploads LavaFlow can't read the code of, and why.
type Unreadable struct {
	// Resources names them, in the order they were recorded.
	Resources []string
	// Uploads is how many uploads were recorded in all.
	Uploads int
	// Reasons says why, each once, in the order found.
	Reasons []Reason
}

// Unreadable returns the uploads whose code LavaFlow can't read, or nil when
// it can read them all as far as the checkout shows, or ctx ends first.
//
// LavaFlow reads an upload's code at a commit Volcano verified: one the upload
// reported clean, from target's repository, that its production branch holds.
// The branch is read from the checkout's remote-tracking branch for origin, as
// of its last fetch. When that branch is missing, or a shallow clone lacks the
// history to tell, the upload counts as readable: Volcano may still verify it.
func (u *Uploads) Unreadable(ctx context.Context, deps cliruntime.Deps, target Target) *Unreadable {
	if u.Len() == 0 {
		return nil
	}
	c := readableCheck{
		git:         gitRunner(deps),
		target:      target,
		absent:      map[string]Reason{},
		uncommitted: map[string][]string{},
		offBranch:   map[string]bool{},
	}
	found := Unreadable{Uploads: len(u.uploads)}
	seen := map[string]bool{}
	for _, up := range u.uploads {
		reason, ok := c.reason(ctx, up)
		if !ok {
			continue
		}
		found.Resources = append(found.Resources, up.resource)
		key := reasonKey(reason)
		if !seen[key] {
			seen[key] = true
			found.Reasons = append(found.Reasons, reason)
		}
	}
	// A git call cut short reads as a reason it isn't.
	if len(found.Resources) == 0 || ctx.Err() != nil {
		return nil
	}
	return &found
}

func reasonKey(r Reason) string {
	return strings.Join(append([]string{strconv.Itoa(int(r.Kind)), r.SHA, r.Repository}, r.Paths...), "\x00")
}

// readableCheck answers for each upload, reading each directory and commit
// once however many uploads share it.
type readableCheck struct {
	git         cliruntime.CommandRunner
	target      Target
	absent      map[string]Reason
	uncommitted map[string][]string
	offBranch   map[string]bool
}

func (c *readableCheck) reason(ctx context.Context, up sentUpload) (Reason, bool) {
	s := up.source
	switch {
	case s == nil:
		return c.whyNone(ctx, up.dir), true
	case !up.reported || up.dirty:
		if paths := c.changes(ctx, s, up.dir); len(paths) > 0 {
			return Reason{Kind: ReasonUncommitted, Paths: paths, SHA: s.sha}, true
		}
		return Reason{Kind: ReasonDiffers, SHA: s.sha}, true
	case c.target.Repository == "":
		return Reason{}, false
	case !strings.EqualFold(s.repository, c.target.Repository):
		return Reason{Kind: ReasonOtherRepository, SHA: s.sha, Repository: s.repository}, true
	case c.notOnBranch(ctx, s):
		return Reason{Kind: ReasonOffBranch, SHA: s.sha}, true
	}
	return Reason{}, false
}

// whyNone is why Detect read no commit in dir. It asks git what Detect
// did, most basic first, so the reason is the first that applies.
func (c *readableCheck) whyNone(ctx context.Context, dir string) Reason {
	if strings.EqualFold(strings.TrimSpace(os.Getenv(OptOutEnv)), "off") {
		return Reason{Kind: ReasonOptedOut}
	}
	if reason, ok := c.absent[dir]; ok {
		return reason
	}
	reason := c.readWhyNone(ctx, dir)
	c.absent[dir] = reason
	return reason
}

func (c *readableCheck) readWhyNone(ctx context.Context, dir string) Reason {
	if _, err := run(ctx, c.git, dir, "rev-parse", "--show-toplevel"); err != nil {
		switch {
		case errors.Is(err, exec.ErrNotFound):
			return Reason{Kind: ReasonNoGit}
		case !insideGitDir(dir):
			return Reason{Kind: ReasonNotCheckout}
		}
		// Such as a checkout git refuses as owned by another user.
		return Reason{Kind: ReasonUnread}
	}
	if _, err := run(ctx, c.git, dir, "rev-parse", "--verify", "--quiet", "HEAD^{commit}"); err != nil {
		return Reason{Kind: ReasonNoCommit}
	}
	remote, err := run(ctx, c.git, dir, "remote", "get-url", "origin")
	if err != nil {
		return Reason{Kind: ReasonNotGitHub}
	}
	if _, err := localgit.ParseGitHubRepository(remote); err != nil {
		return Reason{Kind: ReasonNotGitHub}
	}
	return Reason{Kind: ReasonUnread}
}

// insideGitDir reports whether dir or a directory above it holds .git.
func insideGitDir(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for {
		if _, err := os.Lstat(filepath.Join(abs, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return false
		}
		abs = parent
	}
}

// changes lists what git status shows changed under dir: staged, unstaged
// and untracked paths, an untracked directory as one entry. It is empty when
// git shows nothing or can't be asked.
func (c *readableCheck) changes(ctx context.Context, s *Source, dir string) []string {
	if paths, ok := c.uncommitted[dir]; ok {
		return paths
	}
	var paths []string
	if rel, ok := s.repoPath(dir); ok {
		// --no-optional-locks keeps status from rewriting the index while a
		// build may be using the checkout.
		out, err := run(ctx, s.git, s.root, "--literal-pathspecs", "--no-optional-locks", "-c", "core.fsmonitor=false",
			"status", "--porcelain", "-z", "--no-renames", "--untracked-files=normal", "--", rel)
		if err == nil {
			for entry := range strings.SplitSeq(out, "\x00") {
				// "XY path", paths from the repository root.
				if len(entry) > 3 {
					paths = append(paths, entry[3:])
				}
			}
		}
	}
	c.uncommitted[dir] = paths
	return paths
}

// notOnBranch reports whether the checkout's origin/<branch> exists and
// doesn't hold the commit. It reports false whenever git can't tell: no such
// branch, a git failure, or a shallow clone that may lack the history.
func (c *readableCheck) notOnBranch(ctx context.Context, s *Source) bool {
	if off, ok := c.offBranch[s.sha]; ok {
		return off
	}
	off := c.readNotOnBranch(ctx, s)
	c.offBranch[s.sha] = off
	return off
}

func (c *readableCheck) readNotOnBranch(ctx context.Context, s *Source) bool {
	branch := c.target.Branch
	if branch == "" || strings.HasPrefix(branch, "-") {
		return false
	}
	ref := "refs/remotes/origin/" + branch
	if _, err := run(ctx, s.git, s.root, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
		return false
	}
	out, err := run(ctx, s.git, s.root, "for-each-ref", "--contains", s.sha, "--format=%(refname)", ref)
	if err != nil {
		return false
	}
	for line := range strings.Lines(out) {
		if strings.TrimSpace(line) == ref {
			return false
		}
	}
	shallow, err := run(ctx, s.git, s.root, "rev-parse", "--is-shallow-repository")
	return err == nil && strings.TrimSpace(shallow) == "false"
}
