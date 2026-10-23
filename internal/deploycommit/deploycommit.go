// Package deploycommit reports which commit a cloud deploy's source came from,
// so Volcano can tie the deployment to its repository and LavaFlow can open
// fix pull requests against the code that actually runs.
//
// It sends only the GitHub repository as owner/name, the commit ID, and
// whether the uploaded files differ from that commit: never a remote URL, a
// path, a branch, or a diff. When any part is missing or would not pass the
// server's check, it reports nothing at all. The server refuses a malformed
// commit with a 400 before anything deploys, so sending a doubtful one would
// fail the customer's deploy over a field they never asked for.
package deploycommit

import (
	"context"
	"crypto/sha1" //nolint:gosec // git names a blob by its SHA-1; this matches it, it does not secure anything
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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

// maxHashArgs bounds the paths one `git hash-object` call names, in bytes,
// well under Windows' 32,767-character command line.
const maxHashArgs = 8000

// The server's own checks, so a value it would refuse is never sent.
var (
	shaPattern   = regexp.MustCompile(`^[0-9a-f]{40}$`)
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// Git tree modes.
const (
	modeExecutable = "100755"
	modeSymlink    = "120000"
)

// Source is the commit checked out where a deploy reads its files. Detect
// reads it once per deploy; Commit then reports it for each upload.
type Source struct {
	repository string
	sha        string
	// root is the repository's top-level directory, symlinks resolved.
	root string
	// tree holds each file in the commit by its slash-separated path.
	tree map[string]treeEntry
	// fileMode is whether git tracks the executable bit here.
	fileMode bool
	git      cliruntime.CommandRunner
}

type treeEntry struct {
	mode string
	blob string
}

// Detect reads the commit checked out in dir: origin's GitHub repository, HEAD,
// and the files HEAD holds. It returns nil when it cannot say for sure, and
// in local development, which never reports a commit.
//
// The commit comes from dir itself, in CI as anywhere else. A workflow's
// GITHUB_SHA names the commit that triggered it, which is not what a checkout
// of another ref or repository holds. On a pull_request run HEAD is the merge
// commit GitHub made for the check, which is on no branch, so the server
// records it unverified: true, and nothing more is claimed.
func Detect(ctx context.Context, deps cliruntime.Deps, dir string) *Source {
	if deps.LocalMode || strings.EqualFold(strings.TrimSpace(os.Getenv(OptOutEnv)), "off") {
		return nil
	}
	git := gitRunner(deps)

	remote, err := run(ctx, git, dir, "remote", "get-url", "origin")
	if err != nil {
		return nil
	}
	parsed, err := localgit.ParseGitHubRepository(remote)
	if err != nil {
		return nil
	}
	head, err := run(ctx, git, dir, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	repository, sha := parsed.FullName(), strings.ToLower(strings.TrimSpace(head))
	if !valid(repository, sha) {
		return nil
	}

	toplevel, err := run(ctx, git, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	root, err := filepath.EvalSymlinks(filepath.FromSlash(strings.TrimSpace(toplevel)))
	if err != nil {
		return nil
	}
	tree, err := run(ctx, git, root, "ls-tree", "-r", "-z", "--full-tree", "HEAD")
	if err != nil {
		return nil
	}
	return &Source{
		repository: repository,
		sha:        sha,
		root:       root,
		tree:       parseTree(tree),
		fileMode:   tracksFileMode(ctx, git, root),
		git:        git,
	}
}

// Commit reports the commit for one upload. files are the project files the
// upload holds; reads are the files and directories packaging read from. The
// upload is clean only when each file is in the commit at its own path, with
// the content and executable bit the commit gives it, and no file the commit
// holds under reads is missing. Anything else marks it dirty: an untracked or
// ignored file, an edit git's own status would not show, or a deleted file.
// It returns nil when s is nil or a file cannot be read.
func (s *Source) Commit(ctx context.Context, files, reads []string) *api.DeployCommit {
	if s == nil {
		return nil
	}
	dirty, err := s.differs(ctx, files, reads)
	if err != nil {
		return nil
	}
	return &api.DeployCommit{Repository: s.repository, SHA: s.sha, Dirty: dirty}
}

func (s *Source) differs(ctx context.Context, files, reads []string) (bool, error) {
	// unfiltered are files whose bytes are not HEAD's blob as they are. Git
	// may still store them as that blob: core.autocrlf and clean filters
	// turn a checkout's bytes back into what the commit holds.
	var unfiltered []string
	for _, file := range files {
		path, ok := s.repoPath(file)
		if !ok {
			return true, nil
		}
		entry, ok := s.tree[path]
		if !ok {
			return true, nil
		}
		info, err := os.Lstat(file)
		if err != nil {
			return false, err
		}
		if s.fileMode && entry.mode != modeSymlink && (entry.mode == modeExecutable) != (info.Mode()&0o111 != 0) {
			return true, nil
		}
		blob, err := fileBlobID(file)
		if err != nil {
			return false, err
		}
		if blob != entry.blob {
			unfiltered = append(unfiltered, path)
		}
	}
	blobs, err := s.hashObjects(ctx, unfiltered)
	if err != nil {
		return false, err
	}
	for i, path := range unfiltered {
		if blobs[i] != s.tree[path].blob {
			return true, nil
		}
	}
	return s.missing(reads), nil
}

// missing reports whether a file the commit holds at or under one of reads
// is gone from the working tree, so the upload lacks it.
func (s *Source) missing(reads []string) bool {
	for _, read := range reads {
		prefix, ok := s.repoPath(read)
		if !ok {
			continue
		}
		for path := range s.tree {
			if prefix != "." && path != prefix && !strings.HasPrefix(path, prefix+"/") {
				continue
			}
			if _, err := os.Lstat(filepath.Join(s.root, filepath.FromSlash(path))); err != nil {
				return true
			}
		}
	}
	return false
}

// repoPath is file's slash-separated path in the repository, false when it is
// outside it. A file that no longer exists resolves through its directory.
func (s *Source) repoPath(file string) (string, bool) {
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		dir, dirErr := filepath.EvalSymlinks(filepath.Dir(abs))
		if dirErr != nil {
			return "", false
		}
		resolved = filepath.Join(dir, filepath.Base(abs))
	}
	rel, err := filepath.Rel(s.root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// hashObjects returns the blob ID git would store for each path, applying the
// repository's end-of-line conversion and clean filters.
func (s *Source) hashObjects(ctx context.Context, paths []string) ([]string, error) {
	var blobs []string
	for len(paths) > 0 {
		args, size := []string{"hash-object", "--"}, 0
		for len(paths) > 0 && (size == 0 || size+len(paths[0]) < maxHashArgs) {
			args = append(args, paths[0])
			size += len(paths[0]) + 1
			paths = paths[1:]
		}
		out, err := run(ctx, s.git, s.root, args...)
		if err != nil {
			return nil, err
		}
		ids := strings.Fields(out)
		if len(ids) != len(args)-2 {
			return nil, fmt.Errorf("git hash-object returned %d IDs for %d paths", len(ids), len(args)-2)
		}
		blobs = append(blobs, ids...)
	}
	return blobs, nil
}

// fileBlobID is the ID git gives a blob with the file's bytes as they are.
func fileBlobID(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	return blobID(f, info.Size())
}

// blobID is the ID git gives a blob with this content.
func blobID(content io.Reader, size int64) (string, error) {
	hash := sha1.New() //nolint:gosec // see the import
	fmt.Fprintf(hash, "blob %d\x00", size)
	if _, err := io.Copy(hash, content); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// parseTree reads `git ls-tree -r -z` output: "<mode> <type> <object>\t<path>"
// records separated by NUL. Only blobs are kept; a submodule is a commit.
func parseTree(tree string) map[string]treeEntry {
	entries := map[string]treeEntry{}
	for record := range strings.SplitSeq(tree, "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if ok && len(fields) == 3 && fields[1] == "blob" {
			entries[path] = treeEntry{mode: fields[0], blob: fields[2]}
		}
	}
	return entries
}

// tracksFileMode reports core.fileMode, which git leaves unset where it is
// true. Windows has no executable bit to compare, whatever the setting.
func tracksFileMode(ctx context.Context, git cliruntime.CommandRunner, root string) bool {
	if runtime.GOOS == "windows" {
		return false
	}
	value, err := run(ctx, git, root, "config", "--type=bool", "--get", "core.fileMode")
	return err != nil || strings.TrimSpace(value) != "false"
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
		return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // the command is git; its arguments are constants, the deploy's own directory, and paths inside it
	})
}
