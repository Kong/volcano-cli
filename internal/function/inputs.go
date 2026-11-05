package function

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/Kong/volcano-cli/internal/ignore"
)

const functionsPrefix = "volcano/functions/"

// inputPaths returns whether a slash-separated path under baseDir is a file
// PackageSource reads for info, or would read if a regular file were there,
// by the rules it packaged with: the function's file, or a file in its
// directory that matcher keeps; a dependency manifest the walk from the
// function's directory up to baseDir looks for, unless a nearer one took its
// archive path; or a file in a shared library that matcher keeps and no
// other library of the same name replaced. read holds the slash-separated
// paths under baseDir that packaging read, and libs the libraries it found.
// It returns nil when it cannot place the function under baseDir.
func inputPaths(info SourceInfo, baseDir string, matcher *ignore.Matcher, read map[string]bool, libs []SharedLibrary) func(string) bool {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return nil
	}
	rel := func(p string) (string, bool) {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", false
		}
		r, err := filepath.Rel(base, abs)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(r), true
	}
	source, ok := rel(info.Path)
	if !ok {
		return nil
	}
	sourceDir := path.Dir(source)
	libPaths := make(map[string]string, len(libs))
	for _, lib := range libs {
		if p, ok := rel(lib.Path); ok {
			libPaths[lib.Name] = p
		}
	}

	manifests := map[string]bool{}
	for dir := sourceDir; ; dir = path.Dir(dir) {
		for _, name := range info.Runtime.Deployment.DependencyManifests {
			// Packaging has already refused a name that is not safe.
			manifest, err := safeArchivePath(name)
			if err != nil {
				continue
			}
			// A manifest above the function goes in the archive at its path
			// under baseDir, which for one at baseDir is the path the
			// function's own manifest takes first.
			if dir == "." && sourceDir != "." && read[path.Join(sourceDir, manifest)] {
				continue
			}
			manifests[path.Join(dir, manifest)] = true
		}
		if dir == "." {
			break
		}
	}

	return func(p string) bool {
		switch {
		case manifests[p]:
			return true
		case !info.IsDir:
			if p == source {
				return true
			}
		case strings.HasPrefix(p, sourceDir+"/"):
			return keeps(matcher, sourceDir, sourceDir, p)
		}
		libName, libRoot, ok := sharedLibrary(p)
		if !ok {
			return false
		}
		if found, ok := libPaths[libName]; ok && found != libRoot {
			return false
		}
		if p == libRoot {
			return !matcher.ShouldIgnoreWith(libName, p, false)
		}
		return keeps(matcher, libRoot, "", p)
	}
}

// keeps reports whether readDirectoryWithMatcher, walking the directory at
// walkRoot, reads the file at p under it: matcher leaves out neither p nor a
// directory between them. Patterns match paths under matchRoot, "" for
// baseDir.
func keeps(matcher *ignore.Matcher, walkRoot, matchRoot, p string) bool {
	matchPath := func(q string) string {
		if matchRoot == "" {
			return q
		}
		return strings.TrimPrefix(q, matchRoot+"/")
	}
	for i := len(walkRoot) + 1; i < len(p); i++ {
		if p[i] == '/' && matcher.ShouldIgnoreWith(matchPath(p[:i]), p[:i], true) {
			return false
		}
	}
	return !matcher.ShouldIgnoreWith(matchPath(p), p, false)
}

// sharedLibrary names the library FindSharedLibraries finds p in, if any: an
// entry starting with an underscore at the top of volcano/functions, or at
// any depth elsewhere under volcano. root is the library's path.
func sharedLibrary(p string) (name, root string, ok bool) {
	if rest, found := strings.CutPrefix(p, functionsPrefix); found {
		first, _, _ := strings.Cut(rest, "/")
		if !strings.HasPrefix(first, "_") {
			return "", "", false
		}
		return first, functionsPrefix + first, true
	}
	rest, found := strings.CutPrefix(p, "volcano/")
	if !found {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "_") {
			name = strings.Join(parts[:i+1], "/")
			return name, "volcano/" + name, true
		}
	}
	return "", "", false
}
