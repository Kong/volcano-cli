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
	stagedErr  error
	stagedRead bool
	// untracked lists the files and directories git neither tracks nor
	// ignores, a directory ending in a slash.
	untracked     []string
	untrackedErr  error
	untrackedRead bool
	// repositoryClean is whether the whole repository matches the commit,
	// nil until asked.
	repositoryClean *bool
}

// Inputs are the project files a function upload is made from.
type Inputs struct {
	// Dir is the project directory the deploy reads from.
	Dir string
	// Files are the files the archive holds, recorded as packaging read them.
	Files []File
	// Feeds reports whether a slash-separated path under Dir is a file
	// packaging reads for the function, or would read if a regular file were
	// there, after its ignore rules and which file takes each archive path.
	// nil names nothing, and no commit is reported.
	Feeds func(string) bool
	// ShadowedBy names, for a path packaging would read for the function but
	// for another file or library taking its place, that file or the
	// library's root, slash-separated under Dir; "" for any other path. nil
	// names none.
	ShadowedBy func(string) string
	// Roots are the files and directories packaging resolved the function's
	// files from: its own file or directory and each shared library. Each
	// must be under Dir.
	Roots []string
	// Paths are other files the deploy reads, or would read if they existed,
	// such as the manifest its declarations come from and the .gitignore
	// packaging applies. Each must be under Dir.
	Paths []string
}

// FunctionCommit reports the commit for a function upload, or nil unless the
// function is clean: every committed file it is made from is in the upload
// and, like each of Paths, has the content the commit holds; none of them is
// staged; the upload holds no file the commit lacks unless git ignores it;
// and no file under Roots is untracked unless git ignores it, even one
// packaging leaves out. A committed input the upload lacks, deleted, replaced
// by a symlink or a directory, or behind a symlinked directory, counts as
// changed, and so does one another file or library takes the place of
// unless the commit or the upload holds a file of that replacement: a
// checkout of the commit would upload the committed file.
//
// A file git ignores is build output, made from sources the CLI cannot name.
// An upload that holds one is clean only when the whole repository is too:
// every tracked file matches the commit, executable bit included where git
// tracks it, nothing is staged, no file is untracked unless git ignores it,
// and there is no submodule.
//
// It returns nil when s is nil, an input is outside the repository or Dir,
// or reached through a symlink, a submodule holds an input, or git cannot be
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
	base, err := filepath.Abs(in.Dir)
	if err != nil {
		return false, err
	}
	read := make(map[string]*File, len(in.Files))
	uploaded := make([]string, 0, len(in.Files))
	var uncommitted []string
	for i := range in.Files {
		path, ok, err := s.placed(dir, base, in.Files[i].Path)
		if err != nil || !ok {
			return false, err
		}
		uploaded = append(uploaded, path)
		if _, committed := s.tree[path]; !committed {
			uncommitted = append(uncommitted, path)
			continue
		}
		read[path] = &in.Files[i]
	}
	paths := make(map[string]bool, len(in.Paths))
	for _, file := range in.Paths {
		path, ok, err := s.placed(dir, base, file)
		if err != nil || !ok {
			return false, err
		}
		paths[path] = true
		if _, committed := s.tree[path]; committed {
			continue
		}
		if _, err := os.Lstat(file); err == nil {
			uncommitted = append(uncommitted, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, err
		}
	}
	roots := make([]string, 0, len(in.Roots))
	for _, file := range in.Roots {
		path, ok, err := s.placed(dir, base, file)
		if err != nil || !ok {
			return false, err
		}
		roots = append(roots, path)
	}
	if untracked, err := s.untrackedUnder(ctx, roots); err != nil || untracked {
		return false, err
	}
	built, err := s.ignored(ctx, uncommitted)
	if err != nil {
		return false, err
	}
	if len(built) < len(uncommitted) {
		return false, nil
	}

	feeds := func(path string) bool {
		if read[path] != nil || paths[path] {
			return true
		}
		return within(dir, path) && in.Feeds(underDir(dir, path))
	}
	for path := range s.gitlinks {
		if feeds(path) {
			return false, nil
		}
	}
	staged, err := s.staged(ctx)
	if err != nil || slices.ContainsFunc(staged, feeds) {
		return false, err
	}
	shadowedBy := func(path string) string {
		if in.ShadowedBy == nil || !within(dir, path) {
			return ""
		}
		by := in.ShadowedBy(underDir(dir, path))
		if by == "" || dir == "." {
			return by
		}
		return dir + "/" + by
	}
	replaced := func(by string) bool {
		return s.holds(by) || slices.ContainsFunc(uploaded, func(path string) bool { return within(by, path) })
	}
	if clean, err := s.inputsMatch(ctx, feeds, shadowedBy, replaced, read, paths); err != nil || !clean {
		return false, err
	}
	if len(built) > 0 {
		return s.wholeRepositoryClean(ctx)
	}
	return true, nil
}

// placed is file's repository path, false when it is outside the repository
// or base, or is not where its path under base puts it: a directory a
// symlink stands in for could hide a deleted input, and a symlink could
// stand in for an input with another committed file.
func (s *Source) placed(dir, base, file string) (string, bool, error) {
	path, ok := s.repoPath(file)
	if !ok || !within(dir, path) {
		return "", false, nil
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return "", false, err
	}
	if lexical, relErr := filepath.Rel(base, abs); relErr != nil || filepath.ToSlash(lexical) != underDir(dir, path) {
		return "", false, relErr
	}
	return path, true, nil
}

// inputsMatch reports whether every committed input is as the commit holds
// it: one packaging read, by the bytes it read, and one of paths, from disk.
// Any other committed input is one the upload lacks, and so is a committed
// file whose replacement, as shadowedBy names it, replaced reports missing.
func (s *Source) inputsMatch(
	ctx context.Context, feeds func(string) bool, shadowedBy func(string) string, replaced func(string) bool,
	read map[string]*File, paths map[string]bool,
) (bool, error) {
	var matches []match
	present := map[string]bool{}
	for path, entry := range s.tree {
		if !feeds(path) {
			by := shadowedBy(path)
			if by == "" {
				continue
			}
			ok, seen := present[by]
			if !seen {
				ok = replaced(by)
				present[by] = ok
			}
			if !ok {
				return false, nil
			}
			continue
		}
		file := read[path]
		switch {
		case file != nil && entry.mode == modeSymlink:
			return false, nil
		case file == nil && !paths[path]:
			return false, nil
		case file == nil:
			same, onDisk, err := s.onDisk(path, entry, false)
			if err != nil || !same {
				return false, err
			}
			if onDisk == nil {
				continue
			}
			file = onDisk
		}
		m, same := compare(path, entry, file)
		if !same {
			return false, nil
		}
		if m != nil {
			matches = append(matches, *m)
		}
	}
	return s.attributesAllow(ctx, matches)
}

// wholeRepositoryClean reports whether the whole repository matches the
// commit: no submodule, nothing staged, no file untracked unless git ignores
// it, and every tracked file on disk as the commit holds it.
func (s *Source) wholeRepositoryClean(ctx context.Context) (bool, error) {
	if s.worktree.repositoryClean != nil {
		return *s.worktree.repositoryClean, nil
	}
	// A failure is kept too, so a deploy of many functions reads the
	// repository once whatever the outcome.
	clean, err := s.readWholeRepository(ctx)
	clean = clean && err == nil
	s.worktree.repositoryClean = &clean
	return clean, err
}

func (s *Source) readWholeRepository(ctx context.Context) (bool, error) {
	if len(s.gitlinks) > 0 {
		return false, nil
	}
	staged, err := s.staged(ctx)
	if err != nil || len(staged) > 0 {
		return false, err
	}
	untracked, err := s.untracked(ctx)
	if err != nil || len(untracked) > 0 {
		return false, err
	}
	var matches []match
	for path, entry := range s.tree {
		same, file, err := s.onDisk(path, entry, s.fileMode)
		if err != nil || !same {
			return false, err
		}
		if file == nil {
			continue
		}
		m, same := compare(path, entry, file)
		if !same {
			return false, nil
		}
		if m != nil {
			matches = append(matches, *m)
		}
	}
	return s.attributesAllow(ctx, matches)
}

// compare reports whether file has the content of the commit's entry, and
// the line-ending or Git LFS match whose attributes must allow it.
func compare(path string, entry treeEntry, file *File) (*match, bool) {
	switch entry.blob {
	case file.raw:
		return nil, true
	case file.lf:
		return &match{path: path, binary: file.binary}, true
	case file.lfs:
		return &match{path: path, lfs: true}, true
	}
	return nil, false
}

// onDisk reads the committed file at path from disk. same is false when it is
// gone or no longer the kind of file the commit holds, or, with mode set,
// when its executable bit differs. A symlink is compared here and returns no
// file.
func (s *Source) onDisk(path string, entry treeEntry, mode bool) (same bool, file *File, err error) {
	full := filepath.Join(s.root, filepath.FromSlash(path))
	info, err := os.Lstat(full)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
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
	// git records a file as executable from its owner's execute bit.
	if mode && (entry.mode == modeExecutable) != (info.Mode()&0o100 != 0) {
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

// holds reports whether the commit holds a file at or under the repository
// path.
func (s *Source) holds(path string) bool {
	if _, ok := s.tree[path]; ok {
		return true
	}
	for p := range s.tree {
		if strings.HasPrefix(p, path+"/") {
			return true
		}
	}
	return false
}

// untrackedUnder reports whether git lists a file it neither tracks nor
// ignores at or under any of the repository paths.
func (s *Source) untrackedUnder(ctx context.Context, paths []string) (bool, error) {
	if len(paths) == 0 {
		return false, nil
	}
	untracked, err := s.untracked(ctx)
	if err != nil {
		return false, err
	}
	for _, entry := range untracked {
		dir, isDir := strings.CutSuffix(entry, "/")
		for _, path := range paths {
			if within(path, entry) || isDir && within(dir, path) {
				return true, nil
			}
		}
	}
	return false, nil
}

// untracked lists the files git neither tracks nor ignores across the
// repository, a directory holding nothing else as one entry ending in a
// slash.
func (s *Source) untracked(ctx context.Context) ([]string, error) {
	if s.worktree.untrackedRead {
		return s.worktree.untracked, s.worktree.untrackedErr
	}
	out, err := run(ctx, s.git, s.root, "-c", "core.fsmonitor=false",
		"ls-files", "-z", "--others", "--exclude-standard", "--directory", "--no-empty-directory")
	var untracked []string
	for entry := range strings.SplitSeq(out, "\x00") {
		if entry != "" {
			untracked = append(untracked, entry)
		}
	}
	s.worktree.untracked, s.worktree.untrackedErr, s.worktree.untrackedRead = untracked, err, true
	return untracked, err
}

// staged lists the paths whose index entry differs from the commit: staged
// edits, additions, deletions and mode changes, files added with
// intent-to-add, and unresolved conflicts. Reading the index looks at no file
// on disk.
func (s *Source) staged(ctx context.Context) ([]string, error) {
	if s.worktree.stagedRead {
		return s.worktree.staged, s.worktree.stagedErr
	}
	out, err := run(ctx, s.git, s.root, "-c", "core.fsmonitor=false", "ls-files", "-s", "-z")
	if err != nil {
		s.worktree.stagedErr, s.worktree.stagedRead = err, true
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
