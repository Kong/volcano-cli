package function

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/deploycommit"
	"github.com/Kong/volcano-cli/internal/ignore"
)

// cloudIgnorePatterns returns the patterns excluded from function archives in
// addition to the defaults applied by ignore.NewProjectMatcher.
func cloudIgnorePatterns() []string {
	return []string{
		"node_modules",
		".next",
		"dist",
		"build",
		"python_deps",
		"vendor",
		".venv",
		"venv",
		"site-packages",
	}
}

// Package contains one function source archive ready for API upload.
type Package struct {
	Name        string
	Runtime     string
	Handler     string
	ArchiveData []byte
	Size        int64

	// VariableScope and Variables carry the volcano-config.yaml declaration for
	// this function. Both nil means the manifest declared nothing, so deploy
	// sends neither field and the server keeps the function's stored scope.
	VariableScope *string
	Variables     *[]string

	// Inputs are the project files the archive was made from, for reporting
	// the commit they came from.
	Inputs deploycommit.Inputs
	// Commit is the commit the archive's source came from, nil to report none.
	Commit *api.FunctionCommit
}

// SetCommit sets the commit the package's source came from, from source and
// the manifest files its declaration is read from. It is nil unless the
// function is clean; see deploycommit.Source.FunctionCommit.
func (p *Package) SetCommit(ctx context.Context, source *deploycommit.Source, manifestPaths []string) {
	inputs := p.Inputs
	inputs.Paths = append(slices.Clone(inputs.Paths), manifestPaths...)
	p.Commit = source.FunctionCommit(ctx, inputs)
}

// PackageSource packages a cloud function source bundle with manifests and shared libraries.
func PackageSource(info SourceInfo, baseDir string) (*Package, error) {
	buf := new(bytes.Buffer)
	gz := gzip.NewWriter(buf)
	archive := &archiveWriter{tar: tar.NewWriter(gz), added: map[string]struct{}{}}

	matcher, err := ignore.NewProjectMatcher(baseDir, cloudIgnorePatterns()...)
	if err != nil {
		return nil, fmt.Errorf("failed to compile project ignore rules: %w", err)
	}

	if info.IsDir {
		functionDir := filepath.Dir(info.Path)
		files, err := readDirectoryWithMatcher(functionDir, functionDir, matcher)
		if err != nil {
			return nil, fmt.Errorf("failed to read function directory: %w", err)
		}
		if err := archive.addFiles(functionDir, "", files); err != nil {
			return nil, err
		}
	} else {
		content, err := readRequiredRegularFile(info.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read function file: %w", err)
		}
		entrypoint, err := safeArchivePath(info.Runtime.Deployment.Entrypoint)
		if err != nil {
			return nil, fmt.Errorf("unsafe function entrypoint %q: %w", info.Runtime.Deployment.Entrypoint, err)
		}
		if err := archive.add(entrypoint, info.Path, content); err != nil {
			return nil, err
		}
	}

	if err := addDependencyManifests(archive, info.Path, baseDir, info.Runtime.Deployment.DependencyManifests); err != nil {
		return nil, err
	}

	sharedLibs, err := FindSharedLibraries(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find shared libraries: %w", err)
	}
	for _, lib := range sharedLibs {
		if lib.IsDir {
			files, err := readDirectoryWithMatcher(lib.Path, baseDir, matcher)
			if err == nil {
				err = archive.addFiles(lib.Path, lib.Name, files)
			}
			if err != nil {
				return nil, fmt.Errorf("failed to add shared library %s: %w", lib.Name, err)
			}
			continue
		}
		projectMatchPath, err := matcher.ProjectRelPath(lib.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to match shared file %s: %w", lib.Name, err)
		}
		if matcher.ShouldIgnoreWith(lib.Name, projectMatchPath, false) {
			continue
		}
		content, ok, err := readOptionalRegularFile(lib.Path)
		if err != nil {
			return nil, fmt.Errorf("failed to read shared file %s: %w", lib.Name, err)
		}
		if !ok {
			continue
		}
		if err := archive.add(lib.Name, lib.Path, content); err != nil {
			return nil, fmt.Errorf("failed to add shared file %s: %w", lib.Name, err)
		}
	}

	if err := archive.tar.Close(); err != nil {
		_ = gz.Close()
		return nil, fmt.Errorf("failed to close tar archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip archive: %w", err)
	}

	return &Package{
		Name:        info.Name,
		Runtime:     info.Runtime.Name,
		Handler:     info.Runtime.Deployment.Handler,
		ArchiveData: buf.Bytes(),
		Size:        int64(buf.Len()),
		Inputs: deploycommit.Inputs{
			Dir:   baseDir,
			Files: archive.files,
			Feeds: inputPaths(info, baseDir, matcher, archive.read(baseDir), sharedLibs),
			// The matcher reads it, so it decides what the archive leaves out.
			Paths: []string{filepath.Join(baseDir, ".gitignore")},
		},
	}, nil
}

func readRequiredRegularFile(filePath string) ([]byte, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", filePath)
	}
	return os.ReadFile(filePath)
}

func readOptionalRegularFile(filePath string) ([]byte, bool, error) {
	info, err := os.Lstat(filePath)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, nil
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false, err
	}
	return content, true, nil
}

// archiveWriter writes each archive path once and records the project files
// it writes.
type archiveWriter struct {
	tar   *tar.Writer
	added map[string]struct{}
	files []deploycommit.File
}

// add writes content, read from the project file at sourcePath, at
// archivePath unless something is already there.
func (w *archiveWriter) add(archivePath, sourcePath string, content []byte) error {
	cleanPath := filepath.ToSlash(archivePath)
	if _, ok := w.added[cleanPath]; ok {
		return nil
	}
	if err := w.tar.WriteHeader(&tar.Header{
		Name: cleanPath,
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		return err
	}
	if _, err := w.tar.Write(content); err != nil {
		return err
	}
	w.added[cleanPath] = struct{}{}
	// The archive keeps no executable bit: every entry is 0644.
	w.files = append(w.files, deploycommit.NewFile(sourcePath, content, nil))
	return nil
}

// read is the slash-separated paths under baseDir of the files written.
func (w *archiveWriter) read(baseDir string) map[string]bool {
	read := make(map[string]bool, len(w.files))
	base, err := filepath.Abs(baseDir)
	if err != nil {
		return read
	}
	for _, file := range w.files {
		abs, err := filepath.Abs(file.Path)
		if err != nil {
			continue
		}
		if rel, err := filepath.Rel(base, abs); err == nil {
			read[filepath.ToSlash(rel)] = true
		}
	}
	return read
}

// addFiles writes files, read from under srcRoot by their paths relative to
// it, under destPrefix.
func (w *archiveWriter) addFiles(srcRoot, destPrefix string, files map[string][]byte) error {
	paths := make([]string, 0, len(files))
	for relPath := range files {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)
	for _, relPath := range paths {
		tarPath := relPath
		if destPrefix != "" {
			tarPath = filepath.Join(destPrefix, relPath)
		}
		if err := w.add(tarPath, filepath.Join(srcRoot, relPath), files[relPath]); err != nil {
			return err
		}
	}
	return nil
}

func addDependencyManifests(archive *archiveWriter, funcPath, baseDir string, manifestNames []string) error {
	if len(manifestNames) == 0 {
		return nil
	}

	searchRoot, err := filepath.Abs(baseDir)
	if err != nil {
		return fmt.Errorf("failed to resolve project root: %w", err)
	}
	current, err := filepath.Abs(filepath.Dir(funcPath))
	if err != nil {
		return fmt.Errorf("failed to resolve function path: %w", err)
	}
	functionSourceDir := current

	for {
		for _, name := range manifestNames {
			manifestPath, err := safeArchivePath(name)
			if err != nil {
				return fmt.Errorf("unsafe dependency manifest %q: %w", name, err)
			}
			sourcePath := filepath.Join(current, filepath.FromSlash(manifestPath))
			content, ok, err := readOptionalRegularFile(sourcePath)
			if err != nil {
				return fmt.Errorf("failed to read dependency manifest %s: %w", sourcePath, err)
			}
			if !ok {
				continue
			}
			archivePath := manifestPath
			if current != functionSourceDir {
				if rel, err := filepath.Rel(searchRoot, sourcePath); err == nil {
					archivePath = filepath.ToSlash(rel)
				}
			}
			if err := archive.add(archivePath, sourcePath, content); err != nil {
				return err
			}
		}

		if current == searchRoot {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		rel, err := filepath.Rel(searchRoot, parent)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			break
		}
		current = parent
	}
	return nil //nolint:nilerr // loop exits via break on filepath.Rel error; this is the success return after the walk
}

func safeArchivePath(value string) (string, error) {
	slashPath := filepath.ToSlash(value)
	if slashPath == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(value) || path.IsAbs(slashPath) {
		return "", errors.New("path must be relative")
	}
	if slices.Contains(strings.Split(slashPath, "/"), "..") {
		return "", errors.New("path must stay inside the function archive")
	}
	cleanPath := path.Clean(slashPath)
	if cleanPath == "." {
		return "", errors.New("path is empty")
	}
	return cleanPath, nil
}
