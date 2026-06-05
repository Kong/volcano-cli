package function

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

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
}

// PackageSource packages a cloud function source bundle with manifests and shared libraries.
func PackageSource(info SourceInfo, baseDir string) (*Package, error) {
	buf := new(bytes.Buffer)
	gz := gzip.NewWriter(buf)
	tarWriter := tar.NewWriter(gz)

	matcher, err := ignore.NewProjectMatcher(baseDir, cloudIgnorePatterns()...)
	if err != nil {
		return nil, fmt.Errorf("failed to compile project ignore rules: %w", err)
	}
	addedFiles := map[string]struct{}{}

	if info.IsDir {
		functionDir := filepath.Dir(info.Path)
		files, err := readDirectoryWithMatcher(functionDir, functionDir, matcher)
		if err != nil {
			return nil, fmt.Errorf("failed to read function directory: %w", err)
		}
		if err := addFilesToTar(tarWriter, addedFiles, "", files); err != nil {
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
		if err := addFileToTarOnce(tarWriter, addedFiles, entrypoint, content); err != nil {
			return nil, err
		}
	}

	if err := addDependencyManifests(tarWriter, addedFiles, info.Path, baseDir, info.Runtime.Deployment.DependencyManifests); err != nil {
		return nil, err
	}

	sharedLibs, err := FindSharedLibraries(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find shared libraries: %w", err)
	}
	for _, lib := range sharedLibs {
		if lib.IsDir {
			if err := addDirectoryToTarOnce(tarWriter, addedFiles, lib.Path, baseDir, lib.Name, matcher); err != nil {
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
		if err := addFileToTarOnce(tarWriter, addedFiles, lib.Name, content); err != nil {
			return nil, fmt.Errorf("failed to add shared file %s: %w", lib.Name, err)
		}
	}

	if err := tarWriter.Close(); err != nil {
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

func addFileToTarOnce(tarWriter *tar.Writer, added map[string]struct{}, archivePath string, content []byte) error {
	cleanPath := filepath.ToSlash(archivePath)
	if _, ok := added[cleanPath]; ok {
		return nil
	}
	if err := tarWriter.WriteHeader(&tar.Header{
		Name: cleanPath,
		Mode: 0o644,
		Size: int64(len(content)),
	}); err != nil {
		return err
	}
	if _, err := tarWriter.Write(content); err != nil {
		return err
	}
	added[cleanPath] = struct{}{}
	return nil
}

func addDirectoryToTarOnce(tarWriter *tar.Writer, added map[string]struct{}, srcPath, matchRoot, destPrefix string, matcher *ignore.Matcher) error {
	files, err := readDirectoryWithMatcher(srcPath, matchRoot, matcher)
	if err != nil {
		return err
	}
	return addFilesToTar(tarWriter, added, destPrefix, files)
}

func addFilesToTar(tarWriter *tar.Writer, added map[string]struct{}, destPrefix string, files map[string][]byte) error {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, relPath := range paths {
		tarPath := relPath
		if destPrefix != "" {
			tarPath = filepath.Join(destPrefix, relPath)
		}
		if err := addFileToTarOnce(tarWriter, added, tarPath, files[relPath]); err != nil {
			return err
		}
	}
	return nil
}

func addDependencyManifests(tarWriter *tar.Writer, added map[string]struct{}, funcPath, baseDir string, manifestNames []string) error {
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
			if err := addFileToTarOnce(tarWriter, added, archivePath, content); err != nil {
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
