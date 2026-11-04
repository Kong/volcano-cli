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

// typeScriptExtensions are the files the build compiles, sorted by name, the
// order a directory prefers them in.
var typeScriptExtensions = []string{".cts", ".mts", ".ts"}

// javaScriptModuleExtensions are the files the build runs instead of compiling
// TypeScript with the same name, such as index.mjs beside index.ts.
var javaScriptModuleExtensions = []string{".js", ".mjs", ".cjs"}

// isTypeScriptSource reports whether name is a TypeScript file the build
// compiles. A declaration file such as types.d.ts holds no code; .tsx is not
// compiled.
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
// build runs that instead, so that is the entry.
func (c RuntimeCatalog) typeScriptDirectoryEntry(dirPath string) (apiclient.FunctionRuntimeOption, string, bool) {
	if c.typeScript == nil {
		return apiclient.FunctionRuntimeOption{}, "", false
	}
	entrypoint := c.typeScript.Deployment.Entrypoint
	stem := strings.TrimSuffix(entrypoint, filepath.Ext(entrypoint))
	for _, ext := range typeScriptExtensions {
		if !isRegularFile(filepath.Join(dirPath, stem+ext)) {
			continue
		}
		for _, jsExt := range javaScriptModuleExtensions {
			if js := filepath.Join(dirPath, stem+jsExt); isRegularFile(js) {
				return *c.typeScript, js, true
			}
		}
		return *c.typeScript, filepath.Join(dirPath, stem+ext), true
	}
	return apiclient.FunctionRuntimeOption{}, "", false
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

// resolveTypeScriptFiles drops a single-file TypeScript function, such as
// checkout.ts, when another source takes its name, such as checkout.js. That
// source deployed before Volcano compiled TypeScript, and it still does. Two
// TypeScript files with one name, such as checkout.ts and checkout.mts, are an
// error, as they are on the server: neither is the obvious handler.
func resolveTypeScriptFiles(sources []SourceInfo) ([]SourceInfo, error) {
	typeScriptFile := func(source SourceInfo) bool {
		return !source.IsDir && isTypeScriptSource(filepath.Base(source.Path))
	}
	taken := map[string]bool{}
	for _, source := range sources {
		if !typeScriptFile(source) {
			taken[source.Name] = true
		}
	}
	sources = slices.DeleteFunc(sources, func(source SourceInfo) bool {
		return typeScriptFile(source) && taken[source.Name]
	})

	files := map[string]string{}
	for _, source := range sources {
		if !typeScriptFile(source) {
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

func isRegularFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}
