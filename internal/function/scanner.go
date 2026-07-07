package function

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	apicommon "github.com/Kong/volcano-cli/internal/apiclient/common"
)

// RuntimeCatalog indexes API-provided default runtime metadata for local source detection.
type RuntimeCatalog struct {
	byExtension  map[string]apicommon.FunctionRuntimeOption
	byEntrypoint map[string]apicommon.FunctionRuntimeOption
}

// RuntimeCatalogFromOptions converts the API runtime catalog to scanner/package metadata.
func RuntimeCatalogFromOptions(options []apicommon.FunctionRuntimeOption) RuntimeCatalog {
	catalog := RuntimeCatalog{
		byExtension:  map[string]apicommon.FunctionRuntimeOption{},
		byEntrypoint: map[string]apicommon.FunctionRuntimeOption{},
	}

	for _, option := range options {
		if !option.Default {
			continue
		}
		for _, extension := range option.Deployment.FileExtensions {
			catalog.byExtension[extension] = option
		}
		catalog.byEntrypoint[option.Deployment.Entrypoint] = option
	}

	return catalog
}

func (c RuntimeCatalog) runtimeForFile(filename string) (apicommon.FunctionRuntimeOption, bool) {
	runtime, ok := c.byExtension[filepath.Ext(filename)]
	return runtime, ok
}

func (c RuntimeCatalog) runtimeForEntrypoint(filename string) (apicommon.FunctionRuntimeOption, bool) {
	runtime, ok := c.byEntrypoint[filename]
	return runtime, ok
}

// SourceInfo represents one discovered deployable function source.
type SourceInfo struct {
	Path    string
	Name    string
	Runtime apicommon.FunctionRuntimeOption
	IsDir   bool
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
			runtime, entryFile, ok := detectDirectoryRuntime(fullPath, catalog)
			if ok {
				functions = append(functions, SourceInfo{
					Path:    entryFile,
					Name:    entry.Name(),
					Runtime: runtime,
					IsDir:   true,
				})
			}
			continue
		}

		runtime, ok := catalog.runtimeForFile(entry.Name())
		if !ok {
			continue
		}
		functions = append(functions, SourceInfo{
			Path:    fullPath,
			Name:    strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())),
			Runtime: runtime,
		})
	}

	sort.Slice(functions, func(i, j int) bool {
		return functions[i].Name < functions[j].Name
	})
	return functions, nil
}

func detectDirectoryRuntime(dirPath string, catalog RuntimeCatalog) (apicommon.FunctionRuntimeOption, string, bool) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return apicommon.FunctionRuntimeOption{}, "", false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if runtime, ok := catalog.runtimeForEntrypoint(entry.Name()); ok {
			entryFile := filepath.Join(dirPath, entry.Name())
			info, err := os.Lstat(entryFile)
			if err == nil && info.Mode().IsRegular() {
				return runtime, entryFile, true
			}
		}
	}
	return apicommon.FunctionRuntimeOption{}, "", false
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
