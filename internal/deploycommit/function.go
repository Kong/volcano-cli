package deploycommit

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
)

// worktreeCache holds what FunctionCommit reads from git and disk, so a
// deploy of many functions reads each once.
type worktreeCache struct {
	staged     []string
	stagedRead bool
	untracked  *bool
	// disk holds each committed file read from disk, by its path.
	disk map[string]*diskFile
}

type diskFile struct {
	same bool
	file *File
}

// Inputs are the project files a function upload is made from.
type Inputs struct {
	// Dir is the project directory the deploy reads from.
	Dir string
	// Files are the files the archive holds, recorded as packaging read them.
	Files []File
	// Feeds reports whether a slash-separated path under Dir is a file
	// packaging reads for the function, or would read if it were there. nil
	// names nothing, and no commit is reported.
	Feeds func(string) bool
	// Paths are other files the upload reads, or would read if they existed,
	// such as the manifest its declaration comes from.
	Paths []string
}

// FunctionCommit reports the commit for a function upload, or nil unless the
// function is clean: none of its inputs differs from the commit, in the
// index or on disk, and the upload holds no file the commit lacks unless git
// ignores it. An input is a file the upload holds, or one a deleted file
// would have been, from Feeds and Paths; a file packaging leaves out, such as
// node_modules, is not one.
//
// A file git ignores is build output, made from sources the CLI cannot name.
// An upload that holds one is clean only when the whole repository is: no
// tracked file differs from the commit, nothing is staged, and no file is
// untracked unless git ignores it.
//
// It returns nil when s is nil, an input is outside the repository or
// reached through a symlink, a submodule holds an input, or git cannot be
// asked. Content is compared as Commit compares it, running no git filter.
func (s *Source) FunctionCommit(ctx context.Context, in Inputs) *api.FunctionCommit {
	if s == nil || in.Feeds == nil {
		return nil
	}
	clean, err := s.functionClean(ctx, in)
	if err != nil || !clean {
		return nil
	}
	return &api.FunctionCommit{Repository: s.repository, SHA: s.sha}
}

func (s *Source) functionClean(ctx context.Context, in Inputs) (bool, error) {
	dir, ok := s.repoPath(in.Dir)
	if !ok {
		return false, nil
	}
	read, uncommitted, ok, err := s.readPaths(dir, in)
	if err != nil || !ok {
		return false, err
	}
	paths := make(map[string]bool, len(in.Paths))
	for _, path := range in.Paths {
		repoPath, ok := s.repoPath(path)
		if !ok {
			return false, nil
		}
		paths[repoPath] = true
		if _, committed := s.tree[repoPath]; committed {
			continue
		}
		if _, err := os.Lstat(path); err == nil {
			uncommitted = append(uncommitted, repoPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	built, err := s.ignored(ctx, uncommitted)
	if err != nil {
		return false, err
	}
	if len(built) < len(uncommitted) {
		return false, nil
	}
	whole := len(built) > 0
	feeds := func(path string) bool {
		if whole || read[path] != nil || paths[path] {
			return true
		}
		return within(dir, path) && in.Feeds(underDir(dir, path))
	}

	if whole && len(s.gitlinks) > 0 {
		return false, nil
	}
	for path := range s.gitlinks {
		if feeds(path) {
			return false, nil
		}
	}
	staged, err := s.staged(ctx)
	if err != nil {
		return false, err
	}
	if slices.ContainsFunc(staged, feeds) {
		return false, nil
	}
	if whole {
		if dirty, err := s.hasUntracked(ctx); err != nil || dirty {
			return false, err
		}
	}
	readOnDisk := func(path string) bool { return whole || paths[path] }
	return s.inputsMatch(ctx, feeds, read, readOnDisk)
}

// readPaths maps each file the upload holds by its repository path, and
// lists those the commit lacks. ok is false when one is outside the
// repository, or is not where its path under in.Dir puts it, so a directory
// a symlink stands in for cannot hide a deleted input.
func (s *Source) readPaths(dir string, in Inputs) (read map[string]*File, uncommitted []string, ok bool, err error) {
	base, err := filepath.Abs(in.Dir)
	if err != nil {
		return nil, nil, false, err
	}
	read = make(map[string]*File, len(in.Files))
	for i := range in.Files {
		file := &in.Files[i]
		path, ok := s.repoPath(file.Path)
		if !ok || !within(dir, path) {
			return nil, nil, false, nil
		}
		abs, err := filepath.Abs(file.Path)
		if err != nil {
			return nil, nil, false, err
		}
		if lexical, relErr := filepath.Rel(base, abs); relErr != nil || filepath.ToSlash(lexical) != underDir(dir, path) {
			return nil, nil, false, relErr
		}
		if _, committed := s.tree[path]; !committed {
			uncommitted = append(uncommitted, path)
			continue
		}
		read[path] = file
	}
	return read, uncommitted, true, nil
}

// inputsMatch reports whether every committed input is on disk as the commit
// holds it. A committed input packaging did not record counts only when it
// is gone, or when readOnDisk names it: then it is read from disk.
func (s *Source) inputsMatch(
	ctx context.Context, feeds func(string) bool, read map[string]*File, readOnDisk func(string) bool,
) (bool, error) {
	var matches []match
	for path, entry := range s.tree {
		if !feeds(path) {
			continue
		}
		file := read[path]
		if file == nil {
			info, err := os.Lstat(filepath.Join(s.root, filepath.FromSlash(path)))
			if errors.Is(err, os.ErrNotExist) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if !readOnDisk(path) {
				continue
			}
			same, onDisk, err := s.cachedOnDisk(path, entry, info)
			if err != nil || !same {
				return false, err
			}
			if onDisk == nil {
				continue
			}
			file = onDisk
		} else if entry.mode == modeSymlink {
			return false, nil
		}
		switch entry.blob {
		case file.raw:
		case file.lf:
			matches = append(matches, match{path: path, binary: file.binary})
		case file.lfs:
			matches = append(matches, match{path: path, lfs: true})
		default:
			return false, nil
		}
	}
	return s.attributesAllow(ctx, matches)
}

func (s *Source) cachedOnDisk(path string, entry treeEntry, info os.FileInfo) (bool, *File, error) {
	if cached, ok := s.worktree.disk[path]; ok {
		return cached.same, cached.file, nil
	}
	same, file, err := s.onDisk(path, entry, info)
	if err != nil {
		return false, nil, err
	}
	if s.worktree.disk == nil {
		s.worktree.disk = map[string]*diskFile{}
	}
	s.worktree.disk[path] = &diskFile{same: same, file: file}
	return same, file, nil
}

// onDisk reads the committed file at path from disk. same is false when it is
// no longer the kind of file the commit holds; a symlink is compared here and
// returns no file.
func (s *Source) onDisk(path string, entry treeEntry, info os.FileInfo) (same bool, file *File, err error) {
	full := filepath.Join(s.root, filepath.FromSlash(path))
	if entry.mode == modeSymlink {
		if info.Mode()&os.ModeSymlink == 0 {
			return false, nil, nil
		}
		target, err := os.Readlink(full)
		if err != nil {
			return false, nil, err
		}
		return blobID([]byte(filepath.ToSlash(target))) == entry.blob, nil, nil
	}
	if !info.Mode().IsRegular() {
		return false, nil, nil
	}
	f, err := os.Open(full)
	if err != nil {
		return false, nil, err
	}
	defer func() { _ = f.Close() }()
	recorded, err := Copy(io.Discard, f, info.Size(), full, nil)
	if err != nil {
		return false, nil, err
	}
	return true, &recorded, nil
}

// staged lists the paths whose index entry differs from the commit: staged
// edits, additions, deletions and mode changes, files added with
// intent-to-add, and unresolved conflicts. Reading the index looks at no file
// on disk.
func (s *Source) staged(ctx context.Context) ([]string, error) {
	if s.worktree.stagedRead {
		return s.worktree.staged, nil
	}
	out, err := run(ctx, s.git, s.root, "-c", "core.fsmonitor=false", "ls-files", "-s", "-z")
	if err != nil {
		return nil, err
	}
	var staged []string
	seen := make(map[string]bool, len(s.tree))
	for record := range strings.SplitSeq(out, "\x00") {
		meta, path, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			continue
		}
		mode, object, stage := fields[0], fields[1], fields[2]
		seen[path] = true
		if entry, ok := s.tree[path]; ok && stage == "0" && entry.mode == mode && entry.blob == object {
			continue
		}
		if commit, ok := s.gitlinks[path]; ok && stage == "0" && mode == modeGitlink && commit == object {
			continue
		}
		staged = append(staged, path)
	}
	for path := range s.tree {
		if !seen[path] {
			staged = append(staged, path)
		}
	}
	for path := range s.gitlinks {
		if !seen[path] {
			staged = append(staged, path)
		}
	}
	s.worktree.staged, s.worktree.stagedRead = staged, true
	return staged, nil
}

// ignored returns which of paths, repository paths of files the commit lacks,
// git ignores and does not track. A file in the index, even one added with
// intent-to-add, is not ignored.
func (s *Source) ignored(ctx context.Context, paths []string) (map[string]bool, error) {
	ignored := map[string]bool{}
	for _, batch := range pathBatches(paths) {
		out, err := run(ctx, s.git, s.root, append([]string{
			"--literal-pathspecs", "-c", "core.fsmonitor=false",
			"ls-files", "-z", "--others", "--ignored", "--exclude-standard", "--",
		}, batch...)...)
		if err != nil {
			return nil, err
		}
		for path := range strings.SplitSeq(out, "\x00") {
			if path != "" {
				ignored[path] = true
			}
		}
	}
	return ignored, nil
}

// hasUntracked reports whether the repository has a file git neither tracks
// nor ignores.
func (s *Source) hasUntracked(ctx context.Context) (bool, error) {
	if s.worktree.untracked != nil {
		return *s.worktree.untracked, nil
	}
	out, err := run(ctx, s.git, s.root, "-c", "core.fsmonitor=false",
		"ls-files", "-z", "--others", "--exclude-standard", "--directory", "--no-empty-directory")
	if err != nil {
		return false, err
	}
	untracked := out != ""
	s.worktree.untracked = &untracked
	return untracked, nil
}

// pathBatches splits paths into batches whose arguments stay under
// maxPathArgs.
func pathBatches(paths []string) [][]string {
	var batches [][]string
	for len(paths) > 0 {
		size, n := 0, 0
		for n < len(paths) && (n == 0 || size+len(paths[n]) < maxPathArgs) {
			size += len(paths[n]) + 1
			n++
		}
		batches = append(batches, paths[:n])
		paths = paths[n:]
	}
	return batches
}
