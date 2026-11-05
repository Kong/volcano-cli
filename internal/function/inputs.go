package function

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/Kong/volcano-cli/internal/ignore"
)

const functionsPrefix = "volcano/functions/"

// Packaging writes the function's own files first, then its dependency
// manifests from the nearest directory out, then shared libraries, and the
// first file at an archive path keeps it. A library under volcano/functions
// replaces one of the same name elsewhere under volcano.
const (
	priorityFunction     = 0
	priorityManifest     = 1
	priorityFunctionsLib = 1000
	priorityElsewhereLib = 1001
)

// inputRules are the rules PackageSource read a function's files by.
type inputRules struct {
	isDir bool
	// source is the function's file, and sourceDir its directory, as
	// slash-separated paths under baseDir.
	source, sourceDir string
	entrypoint        string
	manifests         []string
	matcher           *ignore.Matcher
	// functionsLibs names the libraries found under volcano/functions,
	// which replace those of the same name elsewhere under volcano whole.
	functionsLibs map[string]bool
}

// inputPaths returns whether a slash-separated path under baseDir is a file
// PackageSource reads for info, or would read if a regular file were there,
// by the rules it packaged with: the function's file, or a file in its
// directory that matcher keeps; a dependency manifest the walk from the
// function's directory up to baseDir looks for; or a file in a shared library
// that matcher keeps, unless one of the same name under volcano/functions
// replaced its library. A path is not one when the file packaging wrote at
// its archive path comes first by those rules. sources holds, by archive
// path, the slash-separated path under baseDir of each file packaging wrote,
// and libs the libraries it found. It returns nil when it cannot place the
// function under baseDir.
func inputPaths(
	info SourceInfo, baseDir string, matcher *ignore.Matcher, sources map[string]string, libs []SharedLibrary,
) func(string) bool {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return nil
	}
	abs, err := filepath.Abs(info.Path)
	if err != nil {
		return nil
	}
	source, err := filepath.Rel(base, abs)
	if err != nil || source == ".." || strings.HasPrefix(source, ".."+string(filepath.Separator)) {
		return nil
	}
	rules := inputRules{
		isDir:         info.IsDir,
		source:        filepath.ToSlash(source),
		matcher:       matcher,
		functionsLibs: map[string]bool{},
	}
	functionsDir := filepath.Join(base, "volcano", "functions")
	for _, lib := range libs {
		if dir, err := filepath.Abs(filepath.Dir(lib.Path)); err == nil && dir == functionsDir {
			rules.functionsLibs[lib.Name] = true
		}
	}
	rules.sourceDir = path.Dir(rules.source)
	if !info.IsDir {
		// Packaging has already refused an entrypoint that is not safe.
		rules.entrypoint, _ = safeArchivePath(info.Runtime.Deployment.Entrypoint)
	}
	for _, name := range info.Runtime.Deployment.DependencyManifests {
		if manifest, err := safeArchivePath(name); err == nil {
			rules.manifests = append(rules.manifests, manifest)
		}
	}

	return func(p string) bool {
		archive, priority, ok := rules.classify(p)
		if !ok {
			return false
		}
		winner, written := sources[archive]
		if !written || winner == p {
			return true
		}
		_, winnerPriority, ok := rules.classify(winner)
		return !ok || winnerPriority >= priority
	}
}

// classify returns the archive path packaging writes the file at p to, and
// the priority it has there, lowest first, false when packaging would not
// read it.
func (r inputRules) classify(p string) (archive string, priority int, ok bool) {
	consider := func(a string, pr int) {
		if a != "" && (!ok || pr < priority) {
			archive, priority, ok = a, pr, true
		}
	}

	if r.isDir {
		if rest, found := strings.CutPrefix(p, r.sourceDir+"/"); found && keeps(r.matcher, r.sourceDir, r.sourceDir, p) {
			consider(rest, priorityFunction)
		}
	} else if p == r.source {
		consider(r.entrypoint, priorityFunction)
	}

	for _, name := range r.manifests {
		dir := "."
		if p != name {
			d, found := strings.CutSuffix(p, "/"+name)
			if !found {
				continue
			}
			dir = d
		}
		if dir != r.sourceDir && dir != "." && !strings.HasPrefix(r.sourceDir, dir+"/") {
			continue
		}
		manifestArchive := p
		if dir == r.sourceDir {
			manifestArchive = name
		}
		consider(manifestArchive, priorityManifest+depth(r.sourceDir)-depth(dir))
	}

	if name, root, found := sharedLibrary(p); found {
		pr := priorityElsewhereLib
		if strings.HasPrefix(root, functionsPrefix) {
			pr = priorityFunctionsLib
		}
		switch {
		case pr == priorityElsewhereLib && r.functionsLibs[name]:
			// The library of the same name under volcano/functions replaced it.
		case p == root && !r.matcher.ShouldIgnoreWith(name, p, false):
			consider(name, pr)
		case p != root && keeps(r.matcher, root, "", p):
			consider(name+"/"+strings.TrimPrefix(p, root+"/"), pr)
		}
	}
	return archive, priority, ok
}

// depth is how many directories deep a slash-separated directory under
// baseDir is, 0 for baseDir itself.
func depth(dir string) int {
	if dir == "." {
		return 0
	}
	return strings.Count(dir, "/") + 1
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
