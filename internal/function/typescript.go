package function

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// The CLI uploads a TypeScript handler as source and the Volcano build compiles
// it. These rules mirror the server's discovery (volcano-hosting
// internal/gitsource) and its compile step
// (infrastructure/codebuild/function-compile.sh). They live here because the
// runtimes API does not list TypeScript extensions: CLIs released before it
// compiled TypeScript package every listed extension as index.js.

// typeScriptLanguage is the runtime language whose build compiles TypeScript.
const typeScriptLanguage = "nodejs"

// typeScriptExtensions are the files the build compiles, sorted by name.
var typeScriptExtensions = []string{".cts", ".mts", ".ts", ".tsx"}

// javaScriptModuleExtensions are the files the build runs instead of compiling
// TypeScript with the same name, such as index.mjs beside index.ts.
var javaScriptModuleExtensions = []string{".js", ".mjs", ".cjs"}

// isTypeScriptSource reports whether name is a TypeScript file the build
// compiles, JSX in .tsx included. A declaration file such as types.d.ts holds
// no code.
func isTypeScriptSource(name string) bool {
	ext := filepath.Ext(name)
	return slices.Contains(typeScriptExtensions, ext) && filepath.Ext(strings.TrimSuffix(name, ext)) != ".d"
}

// typeScriptRuntimeForFile returns the runtime a single-file TypeScript
// function, such as checkout.ts, deploys on.
func (c RuntimeCatalog) typeScriptRuntimeForFile(name string) (apiclient.FunctionRuntimeOption, bool) {
	if c.typeScript == nil || !isTypeScriptSource(name) {
		return apiclient.FunctionRuntimeOption{}, false
	}
	return *c.typeScript, true
}

// typeScriptDirectoryEntry returns a directory function's TypeScript entry:
// the runtime entrypoint's name with a TypeScript extension, such as index.ts.
// When JavaScript with the same name sits beside it, such as index.mjs, the
// build runs that instead, so that is the entry. indexes lists every
// TypeScript index the build would compile, by name; more than one, such as
// index.ts and index.tsx, fails the build, so resolveTypeScriptSources refuses
// the directory unless another source takes its name.
func (c RuntimeCatalog) typeScriptDirectoryEntry(dirPath string) (runtime apiclient.FunctionRuntimeOption, entry string, indexes []string, ok bool) {
	if c.typeScript == nil {
		return apiclient.FunctionRuntimeOption{}, "", nil, false
	}
	entrypoint := c.typeScript.Deployment.Entrypoint
	stem := strings.TrimSuffix(entrypoint, filepath.Ext(entrypoint))
	for _, ext := range typeScriptExtensions {
		if isRegularFile(filepath.Join(dirPath, stem+ext)) {
			indexes = append(indexes, stem+ext)
		}
	}
	if len(indexes) == 0 {
		return apiclient.FunctionRuntimeOption{}, "", nil, false
	}
	for _, jsExt := range javaScriptModuleExtensions {
		if js := filepath.Join(dirPath, stem+jsExt); isRegularFile(js) {
			return *c.typeScript, js, nil, true
		}
	}
	return *c.typeScript, filepath.Join(dirPath, indexes[0]), indexes, true
}

// singleFileEntrypoint is the archive path a single-file function's source
// takes: the runtime's entrypoint, such as index.js, keeping a TypeScript
// file's extension so the build compiles it, such as index.ts.
func singleFileEntrypoint(info SourceInfo) string {
	entrypoint := info.Runtime.Deployment.Entrypoint
	if name := filepath.Base(info.Path); isTypeScriptSource(name) {
		return strings.TrimSuffix(entrypoint, filepath.Ext(entrypoint)) + filepath.Ext(name)
	}
	return entrypoint
}

// resolveTypeScriptSources settles which source deploys when a TypeScript
// source shares its function name with another. A source that deployed before
// Volcano compiled TypeScript, such as hello.js, always wins, so TypeScript
// never changes what an existing function deploys. A directory found through
// its TypeScript index then beats a single TypeScript file, as on the server.
// Two TypeScript files with one name, such as hello.ts and hello.tsx, are an
// error, as they are on the server: neither is the obvious handler. So is a
// directory with two TypeScript indexes, such as index.ts and index.tsx.
func resolveTypeScriptSources(sources []SourceInfo) ([]SourceInfo, error) {
	best := map[string]int{}
	for _, source := range sources {
		if rank, ok := best[source.Name]; !ok || typeScriptRank(source) < rank {
			best[source.Name] = typeScriptRank(source)
		}
	}
	sources = slices.DeleteFunc(sources, func(source SourceInfo) bool {
		return source.viaTypeScript && typeScriptRank(source) > best[source.Name]
	})

	files := map[string]string{}
	for _, source := range sources {
		if !source.viaTypeScript {
			continue
		}
		if source.IsDir {
			if len(source.typeScriptIndexes) > 1 {
				return nil, fmt.Errorf("function %q has more than one TypeScript index, %s, in volcano/functions/%s; keep one",
					source.Name, strings.Join(source.typeScriptIndexes, " and "), source.Name)
			}
			continue
		}
		file := filepath.Base(source.Path)
		if other, ok := files[source.Name]; ok {
			return nil, fmt.Errorf("function %q has two TypeScript files, %s and %s, in volcano/functions; keep one", source.Name, other, file)
		}
		files[source.Name] = file
	}
	return sources, nil
}

// typeScriptRank orders the sources that can share a function name, lowest
// first: one the CLI found before it read TypeScript, a directory found through
// its TypeScript index, then a single TypeScript file.
func typeScriptRank(source SourceInfo) int {
	switch {
	case !source.viaTypeScript:
		return 0
	case source.IsDir:
		return 1
	default:
		return 2
	}
}

func isRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
