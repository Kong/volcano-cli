package function

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// RuntimeCatalog indexes API-provided default runtime metadata for local source detection.
type RuntimeCatalog struct {
	byExtension  map[string]apiclient.FunctionRuntimeOption
	byEntrypoint map[string]apiclient.FunctionRuntimeOption
	// typeScript is the runtime a TypeScript handler deploys on, nil when the
	// catalog has no default Node.js runtime.
	typeScript *apiclient.FunctionRuntimeOption
}

// RuntimeCatalogFromOptions converts the API runtime catalog to scanner/package metadata.
func RuntimeCatalogFromOptions(options []apiclient.FunctionRuntimeOption) RuntimeCatalog {
	catalog := RuntimeCatalog{
		byExtension:  map[string]apiclient.FunctionRuntimeOption{},
		byEntrypoint: map[string]apiclient.FunctionRuntimeOption{},
	}

	for _, option := range options {
		if !option.Default {
			continue
		}
		for _, extension := range option.Deployment.FileExtensions {
			catalog.byExtension[extension] = option
		}
		catalog.byEntrypoint[option.Deployment.Entrypoint] = option
		if option.Language == typeScriptLanguage {
			catalog.typeScript = &option
		}
	}

	return catalog
}

func (c RuntimeCatalog) runtimeForFile(filename string) (apiclient.FunctionRuntimeOption, bool) {
	runtime, ok := c.byExtension[filepath.Ext(filename)]
	return runtime, ok
}

func (c RuntimeCatalog) runtimeForEntrypoint(filename string) (apiclient.FunctionRuntimeOption, bool) {
	runtime, ok := c.byEntrypoint[filename]
	return runtime, ok
}

// SourceInfo represents one discovered deployable function source.
type SourceInfo struct {
	Path    string
	Name    string
	Runtime apiclient.FunctionRuntimeOption
	IsDir   bool
	// viaTypeScript marks a source found only through TypeScript, such as
	// hello.ts or a directory with index.ts, which the CLI used to skip.
	viaTypeScript bool
}

// ScanSources scans volcano/functions for top-level function files and directories.
func ScanSources(baseDir string, catalog RuntimeCatalog) ([]SourceInfo, error) {
	functionsDir := filepath.Join(baseDir, "volcano", "functions")
	entries, err := os.ReadDir(functionsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	functions := make([]SourceInfo, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "_") {
			continue
		}
		fullPath := filepath.Join(functionsDir, entry.Name())
		if entry.IsDir() {
			runtime, entryFile, viaTypeScript, ok := detectDirectoryRuntime(fullPath, catalog)
			if ok {
				functions = append(functions, SourceInfo{
					Path:          entryFile,
					Name:          entry.Name(),
					Runtime:       runtime,
					IsDir:         true,
					viaTypeScript: viaTypeScript,
				})
			}
			continue
		}

		runtime, ok := catalog.runtimeForFile(entry.Name())
		viaTypeScript := false
		if !ok {
			runtime, ok = catalog.typeScriptRuntimeForFile(entry.Name())
			viaTypeScript = ok
		}
		if !ok {
			continue
		}
		functions = append(functions, SourceInfo{
			Path:          fullPath,
			Name:          strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Runtime:       runtime,
			viaTypeScript: viaTypeScript,
		})
	}

	functions, err = resolveTypeScriptSources(functions)
	if err != nil {
		return nil, err
	}
	sort.Slice(functions, func(i, j int) bool {
		return functions[i].Name < functions[j].Name
	})
	return functions, nil
}

// FindTarget returns the scanned source a --file value names. A path to a
// source's file wins over a function name, so -f volcano/functions/orders/index.js
// deploys orders even when another function is named index.
func FindTarget(sources []SourceInfo, target, baseDir string) (SourceInfo, bool) {
	target = strings.TrimSpace(target)
	for _, source := range sources {
		if matchesPath(source, target, baseDir) {
			return source, true
		}
	}
	name := normalizeTarget(target)
	for _, source := range sources {
		if source.Name == name {
			return source, true
		}
	}
	return SourceInfo{}, false
}

// matchesPath reports whether target is the path to a scanned source's file.
func matchesPath(source SourceInfo, target, baseDir string) bool {
	sourcePath, err := filepath.Abs(source.Path)
	if err != nil {
		return false
	}
	targetPath := target
	if !filepath.IsAbs(targetPath) {
		targetPath = filepath.Join(baseDir, targetPath)
	}
	targetPath, err = filepath.Abs(targetPath)
	if err != nil {
		return false
	}
	return filepath.Clean(sourcePath) == filepath.Clean(targetPath)
}

// FormatSourceNames lists scanned source names for an error that has to say
// what was available.
func FormatSourceNames(sources []SourceInfo) string {
	names := make([]string, len(sources))
	for i, source := range sources {
		names[i] = source.Name
	}
	return strings.Join(names, ", ")
}

func normalizeTarget(target string) string {
	name := filepath.Base(strings.TrimSpace(target))
	if extension := filepath.Ext(name); extension != "" {
		name = strings.TrimSuffix(name, extension)
	}
	return name
}

// detectDirectoryRuntime chooses a directory function's entry: a runtime
// entrypoint such as index.js, main.py or main.rb, else a TypeScript index.
// TypeScript ranks last so it never changes the runtime of a directory that
// already deploys, such as main.py beside index.ts. viaTypeScript reports the
// TypeScript index found it.
func detectDirectoryRuntime(dirPath string, catalog RuntimeCatalog) (runtime apiclient.FunctionRuntimeOption, entryFile string, viaTypeScript, ok bool) {
	if runtime, entryFile, ok := detectEntrypoint(dirPath, catalog); ok {
		return runtime, entryFile, false, true
	}
	runtime, entryFile, ok = catalog.typeScriptDirectoryEntry(dirPath)
	return runtime, entryFile, ok, ok
}

// detectEntrypoint finds the first file, by name, named as a runtime's
// entrypoint, such as index.js or main.py.
func detectEntrypoint(dirPath string, catalog RuntimeCatalog) (apiclient.FunctionRuntimeOption, string, bool) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return apiclient.FunctionRuntimeOption{}, "", false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if runtime, ok := catalog.runtimeForEntrypoint(entry.Name()); ok {
			entryFile := filepath.Join(dirPath, entry.Name())
			if isRegularFile(entryFile) {
				return runtime, entryFile, true
			}
		}
	}
	return apiclient.FunctionRuntimeOption{}, "", false
}

// SharedLibrary represents a shared function library file or directory.
type SharedLibrary struct {
	Path  string
	Name  string
	IsDir bool
}

// FindSharedLibraries finds underscore-prefixed files and directories below volcano/ and volcano/functions/.
func FindSharedLibraries(baseDir string) ([]SharedLibrary, error) {
	var shared []SharedLibrary
	indexByName := map[string]int{}
	volcanoDir := filepath.Join(baseDir, "volcano")
	functionsDir := filepath.Join(volcanoDir, "functions")
	searchDirs := []string{
		volcanoDir,
		functionsDir,
	}

	for _, dir := range searchDirs {
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if dir == volcanoDir && path == functionsDir {
				return fs.SkipDir
			}
			if path == dir {
				return nil
			}
			if !strings.HasPrefix(entry.Name(), "_") {
				if dir == functionsDir && entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			relName, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			lib := SharedLibrary{
				Path:  path,
				Name:  filepath.ToSlash(relName),
				IsDir: entry.IsDir(),
			}
			if idx, ok := indexByName[lib.Name]; ok {
				shared[idx] = lib
			} else {
				indexByName[lib.Name] = len(shared)
				shared = append(shared, lib)
			}
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Slice(shared, func(i, j int) bool {
		return shared[i].Name < shared[j].Name
	})
	return shared, nil
}
