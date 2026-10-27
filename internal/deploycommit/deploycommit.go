// Package deploycommit reports which commit a frontend deploy's source came
// from, so Volcano can tie the deployment to its repository and LavaFlow can
// open fix pull requests against the code that actually runs.
//
// It sends only the GitHub repository as owner/name, the commit ID, whether
// the uploaded files differ from that commit, and the uploaded directory's
// path in the repository: never a remote URL, a local path, a branch, or a
// diff. When any part is missing or would not pass the server's check, it
// reports nothing at all. The server refuses a malformed
// commit with a 400 before anything deploys, so sending a doubtful one would
// fail the customer's deploy over a field they never asked for.
package deploycommit

import (
	"context"
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

// maxPathArgs bounds the paths one `git check-attr` call names, in bytes,
// well under Windows' 32,767-character command line.
const maxPathArgs = 8000

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
// reads it; Commit then reports it for the upload.
type Source struct {
	repository string
	sha        string
	// root is the repository's top-level directory, symlinks resolved.
	root string
	// tree holds each file in the commit by its slash-separated path.
	tree map[string]treeEntry
	// fileMode is whether git tracks the executable bit here.
	fileMode bool
	// autocrlf is whether core.autocrlf has git store CRLF line endings
	// as LF.
	autocrlf bool
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
	// The tree is read at the commit already read, never HEAD again, so a
	// checkout that moves meanwhile cannot pair one commit with another's
	// files.
	tree, err := run(ctx, git, root, "ls-tree", "-r", "-z", "--full-tree", sha)
	if err != nil {
		return nil
	}
	return &Source{
		repository: repository,
		sha:        sha,
		root:       root,
		tree:       parseTree(tree),
		fileMode:   tracksFileMode(ctx, git, root),
		autocrlf:   autocrlfOn(ctx, git, root),
		git:        git,
	}
}

// Commit reports the commit for an upload of the directory root, whose
// archive holds files at their paths under it and leaves out the files in
// excluded. The upload is clean only when each file is in the commit at its
// own path, with the content the commit gives it and, where the archive keeps
// one, the same executable bit, and no file the commit holds under root is
// missing. Anything else marks it dirty: an untracked or ignored file, an edit
// git's own status would not show, a deleted or excluded file, or a symlink or
// directory where the commit has a file, which the archive never holds. It
// returns nil when s is nil, root is outside the repository, or git cannot be
// asked.
//
// Content is compared without running git: a clean or smudge filter is a
// program the repository's configuration names. Line endings git converts,
// and Git LFS pointers where the filter attribute names Git LFS, are compared
// here instead; content another filter changes reads as changed.
func (s *Source) Commit(ctx context.Context, root string, files []File, excluded []string) *api.DeployCommit {
	if s == nil {
		return nil
	}
	dir, ok := s.repoPath(root)
	if !ok {
		return nil
	}
	dirty, err := s.differs(ctx, dir, files, excluded)
	if err != nil {
		return nil
	}
	if dir == "." {
		dir = ""
	}
	return &api.DeployCommit{Repository: s.repository, SHA: s.sha, Dirty: dirty, Root: dir}
}

func (s *Source) differs(ctx context.Context, dir string, files []File, excluded []string) (bool, error) {
	// matches are files whose content matches the commit only if git
	// converts their line endings or Git LFS stores them, which their
	// attributes say.
	var matches []match
	for _, file := range files {
		path, ok := s.repoPath(file.Path)
		if !ok || !within(dir, path) {
			return true, nil
		}
		entry, ok := s.tree[path]
		if !ok || entry.mode == modeSymlink {
			return true, nil
		}
		if file.Executable != nil && s.fileMode && (entry.mode == modeExecutable) != *file.Executable {
			return true, nil
		}
		switch entry.blob {
		case file.raw:
		case file.lf:
			matches = append(matches, match{path: path, binary: file.binary})
		case file.lfs:
			matches = append(matches, match{path: path, lfs: true})
		default:
			return true, nil
		}
	}
	ok, err := s.attributesAllow(ctx, matches)
	if err != nil {
		return false, err
	}
	if !ok {
		return true, nil
	}
	return s.missing(dir, excluded), nil
}

// match is a file whose content matches the commit with its line endings
// converted, or as a Git LFS pointer.
type match struct {
	path string
	// binary is whether git's text=auto leaves the file as it is.
	binary bool
	lfs    bool
}

// missing reports whether the upload lacks a file the commit holds under dir:
// the commit's file is a symlink, or the working tree has no regular file at
// its path, or the file is excluded. The archive holds regular files only.
func (s *Source) missing(dir string, excluded []string) bool {
	left := make(map[string]bool, len(excluded))
	for _, file := range excluded {
		if path, ok := s.repoPath(file); ok {
			left[path] = true
		}
	}
	for path, entry := range s.tree {
		if !within(dir, path) {
			continue
		}
		if entry.mode == modeSymlink || left[path] {
			return true
		}
		info, err := os.Lstat(filepath.Join(s.root, filepath.FromSlash(path)))
		if err != nil || !info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

// within reports whether the repository path is at or under dir, "." being
// the repository root.
func within(dir, path string) bool {
	return dir == "." || path == dir || strings.HasPrefix(path, dir+"/")
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

// attributesAllow reports whether git stores each match as the commit holds
// it: with its CRLF line endings as LF, from its text, crlf and eol
// attributes or, when they say nothing, core.autocrlf; or as a Git LFS
// pointer, from its filter attribute. Reading attributes runs nothing they
// name.
func (s *Source) attributesAllow(ctx context.Context, matches []match) (bool, error) {
	for len(matches) > 0 {
		args, size := []string{"check-attr", "-z", "text", "eol", "crlf", "filter", "--"}, 0
		batch := matches
		for len(matches) > 0 && (size == 0 || size+len(matches[0].path) < maxPathArgs) {
			args = append(args, matches[0].path)
			size += len(matches[0].path) + 1
			matches = matches[1:]
		}
		batch = batch[:len(batch)-len(matches)]
		out, err := run(ctx, s.git, s.root, args...)
		if err != nil {
			return false, err
		}
		attrs := attributes(out)
		for _, m := range batch {
			if m.lfs {
				if attrs[m.path]["filter"] != "lfs" {
					return false, nil
				}
				continue
			}
			switch lineEndings(attrs[m.path], s.autocrlf) {
			case convertText:
			case convertAuto:
				if m.binary {
					return false, nil
				}
			default:
				return false, nil
			}
		}
	}
	return true, nil
}

// lineEnding is how git treats a file's line endings when it stores it.
type lineEnding int

const (
	convertUnset lineEnding = iota
	// convertNone leaves them as they are.
	convertNone
	// convertText turns CRLF into LF.
	convertText
	// convertAuto turns CRLF into LF in a file git does not judge binary.
	convertAuto
)

// lineEndings is what git does with a file's line endings, from its text,
// legacy crlf and eol attributes and core.autocrlf, as git's convert.c
// decides it.
func lineEndings(attrs map[string]string, autocrlf bool) lineEnding {
	action := lineEndingAttribute(attrs["text"])
	if action == convertUnset {
		action = lineEndingAttribute(attrs["crlf"])
	}
	if eol := attrs["eol"]; action == convertUnset && (eol == "lf" || eol == "crlf") {
		action = convertText
	}
	if action == convertUnset && autocrlf {
		action = convertAuto
	}
	if action == convertUnset {
		action = convertNone
	}
	return action
}

func lineEndingAttribute(value string) lineEnding {
	switch value {
	case "set", "input":
		return convertText
	case "unset":
		return convertNone
	case "auto":
		return convertAuto
	}
	return convertUnset
}

// attributes reads `git check-attr -z` output: "<path>\0<attribute>\0<value>\0"
// for each path and attribute.
func attributes(out string) map[string]map[string]string {
	fields := strings.Split(out, "\x00")
	attrs := map[string]map[string]string{}
	for i := 0; i+2 < len(fields); i += 3 {
		path, name, value := fields[i], fields[i+1], fields[i+2]
		if attrs[path] == nil {
			attrs[path] = map[string]string{}
		}
		attrs[path][name] = value
	}
	return attrs
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

// autocrlfOn reports whether core.autocrlf has git store CRLF line endings
// as LF where no attribute says otherwise.
func autocrlfOn(ctx context.Context, git cliruntime.CommandRunner, root string) bool {
	value, err := run(ctx, git, root, "config", "--get", "core.autocrlf")
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "input", "yes", "on", "1":
		return true
	}
	return false
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
	// Replacement refs would have git read another commit's objects in place
	// of the ones the reported commit ID names.
	out, err := git.Run(ctx, "git", append([]string{"--no-replace-objects", "-C", dir}, args...)...)
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
