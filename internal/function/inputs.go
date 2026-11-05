package function

import (
	"path"
	"path/filepath"
	"strings"
)

// inputPaths returns whether a slash-separated path under baseDir is a file
// PackageSource reads for info, or would read if it were there: one in the
// function's directory, or the function's file; a dependency manifest the
// walk from the function's directory up to baseDir looks for; or one in a
// shared library. It names files the packager's ignore rules leave out too.
// It returns nil when it cannot place the function under baseDir.
func inputPaths(info SourceInfo, baseDir string) func(string) bool {
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return nil
	}
	source, err := filepath.Abs(info.Path)
	if err != nil {
		return nil
	}
	rel := func(p string) (string, bool) {
		r, err := filepath.Rel(base, p)
		if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(r), true
	}
	sourceDir, ok := rel(filepath.Dir(source))
	if !ok {
		return nil
	}
	var functionDir, functionFile string
	if info.IsDir {
		functionDir = sourceDir
	} else {
		functionFile, _ = rel(source)
	}

	manifests := map[string]bool{}
	for dir := sourceDir; ; dir = path.Dir(dir) {
		for _, name := range info.Runtime.Deployment.DependencyManifests {
			// Packaging has already refused a name that is not safe.
			if manifest, err := safeArchivePath(name); err == nil {
				manifests[path.Join(dir, manifest)] = true
			}
		}
		if dir == "." {
			break
		}
	}

	return func(p string) bool {
		switch {
		case functionFile != "" && p == functionFile:
			return true
		case functionDir != "" && (functionDir == "." || p == functionDir || strings.HasPrefix(p, functionDir+"/")):
			return true
		case manifests[p]:
			return true
		}
		return inSharedLibrary(p)
	}
}

// inSharedLibrary reports whether a slash-separated path under the project
// directory is in a library FindSharedLibraries finds: an entry starting with
// an underscore at the top of volcano/functions, or at any depth elsewhere
// under volcano.
func inSharedLibrary(p string) bool {
	if rest, ok := strings.CutPrefix(p, "volcano/functions/"); ok {
		return strings.HasPrefix(rest, "_")
	}
	rest, ok := strings.CutPrefix(p, "volcano/")
	if !ok {
		return false
	}
	for part := range strings.SplitSeq(rest, "/") {
		if strings.HasPrefix(part, "_") {
			return true
		}
	}
	return false
}
