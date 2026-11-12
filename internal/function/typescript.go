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

// typeScriptExtensions are the files the build compiles, in the order a
// directory prefers them: by name, with .tsx last. The build compiled .ts,
// .mts and .cts before it compiled .tsx, so an index.tsx or hello.tsx beside
// one of them never changes what deploys.
var typeScriptExtensions = []string{".cts", ".mts", ".ts", jsxExtension}

// jsxExtension is the TypeScript the build compiles JSX in. It ranks after the
// other TypeScript extensions.
const jsxExtension = ".tsx"

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
// build runs that instead, so that is the entry. index.ts beats index.tsx, as
// in the build. The directory uploads whole, so the build, not the scan,
// refuses two indexes of one rank, such as index.ts and index.mts, and the scan
// never blocks deploying another function. viaJSX reports that index.tsx found
// the directory, which ranks it after other TypeScript even when JavaScript is
// its entry.
func (c RuntimeCatalog) typeScriptDirectoryEntry(dirPath string) (runtime apiclient.FunctionRuntimeOption, entry string, viaJSX, ok bool) {
	if c.typeScript == nil {
		return apiclient.FunctionRuntimeOption{}, "", false, false
	}
	entrypoint := c.typeScript.Deployment.Entrypoint
	stem := strings.TrimSuffix(entrypoint, filepath.Ext(entrypoint))
	for _, ext := range typeScriptExtensions {
		if !isRegularFile(filepath.Join(dirPath, stem+ext)) {
			continue
		}
		viaJSX = ext == jsxExtension
		for _, jsExt := range javaScriptModuleExtensions {
			if js := filepath.Join(dirPath, stem+jsExt); isRegularFile(js) {
				return *c.typeScript, js, viaJSX, true
			}
		}
		return *c.typeScript, filepath.Join(dirPath, stem+ext), viaJSX, true
	}
	return apiclient.FunctionRuntimeOption{}, "", false, false
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
// never changes what an existing function deploys. .ts, .mts or .cts then
// beats .tsx, and a directory beats a single file, as typeScriptRank orders
// them on the server too. Two TypeScript files of one rank with one name, such
// as hello.ts and hello.mts, are an error, as they are on the server: neither
// is the obvious handler.
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
		if !source.viaTypeScript || source.IsDir {
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
// first: one the CLI found before it read TypeScript; a directory with a .ts,
// .mts or .cts index; a .ts, .mts or .cts file; a directory with an index.tsx;
// then a .tsx file. A directory ranks by the TypeScript index that found it,
// even when JavaScript beside it is the entry: index.mjs beside index.tsx
// ranks as a directory with an index.tsx.
func typeScriptRank(source SourceInfo) int {
	if !source.viaTypeScript {
		return 0
	}
	rank := 1
	if source.viaJSX {
		rank = 3
	}
	if !source.IsDir {
		rank++
	}
	return rank
}

func isRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
