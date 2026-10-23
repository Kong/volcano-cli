package frontend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kong/volcano-cli/internal/ignore"
)

const (
	frontendAppPathMarker = ".volcano/frontend-app-path"
	// maxFrontendPackageFiles caps how many files we will package into a
	// single archive. The whole archive is built in memory, so a missing
	// .gitignore or stray cache directory should not be able to OOM the CLI.
	maxFrontendPackageFiles = 10000
)

// cloudIgnorePatterns returns the patterns excluded from frontend archives in
// addition to the defaults applied by ignore.NewProjectMatcher.
func cloudIgnorePatterns() []string {
	return []string{
		"node_modules",
		".next",
		"dist",
		"build",
		// Next.js convention for machine-local secrets; never ship by default.
		".env.local",
		".env*.local",
		// Volcano env files hold project variables, the service key among
		// them, whether or not the project's .gitignore lists them.
		"volcano/*.env",
		"volcano.env",
	}
}

// Package contains one frontend source archive ready for API upload.
type Package struct {
	Archive       []byte
	Size          int64
	PackagingRoot string
	AppPath       string
	// SkippedSymlinks lists POSIX-relative archive paths whose symlinks were
	// dropped because the archive cannot represent them safely. Callers
	// should surface these so users notice missing files (e.g. monorepo
	// assets symlinked across packages).
	SkippedSymlinks []string
	// Sources are the project files the archive holds, each read as is. The
	// app-path marker is written by the CLI, so it is not one of them.
	Sources []string
}

// PackageOptions tunes how PackageDirectory builds the archive.
type PackageOptions struct {
	// DisableWorkspacePromotion packages the selected root as-is even when
	// it participates in a JS workspace with `workspace:` deps. The caller
	// is responsible for telling the server where the app lives.
	DisableWorkspacePromotion bool
	// AppRoot, when non-empty, is the POSIX-relative path the caller plans
	// to send as the server-side `app_root` field. PackageDirectory rejects
	// the request when this path would be excluded by the archive ignore
	// rules, so the server is never told to build a directory that is not
	// in the upload.
	AppRoot string
	// ExcludeFiles are paths of files to leave out of the archive. Those
	// outside the packaging root are ignored.
	ExcludeFiles []string
}

// PackageDirectory builds a tar.gz archive of rootDir for upload, applying
// ignore rules and optionally promoting the working tree to a workspace root.
// Promotion can be disabled via opts.DisableWorkspacePromotion.
func PackageDirectory(rootDir string, opts PackageOptions) (*Package, error) {
	packagingRoot, appPath, err := resolveRoots(rootDir, opts)
	if err != nil {
		return nil, err
	}

	matcher, err := ignore.NewProjectMatcher(packagingRoot, cloudIgnorePatterns()...)
	if err != nil {
		return nil, err
	}

	if opts.AppRoot != "" && matcher.ShouldIgnore(opts.AppRoot, true) {
		return nil, fmt.Errorf("--app-root %q is excluded by the archive ignore rules; rename the directory or remove it from .gitignore", opts.AppRoot)
	}

	if err := checkAppPathMarkerCollision(packagingRoot, appPath, matcher); err != nil {
		return nil, err
	}

	excluded, err := archivePaths(packagingRoot, opts.ExcludeFiles)
	if err != nil {
		return nil, err
	}
	data, skipped, sources, err := buildArchive(packagingRoot, appPath, matcher, excluded)
	if err != nil {
		return nil, err
	}

	return &Package{
		Archive:         data,
		Size:            int64(len(data)),
		PackagingRoot:   packagingRoot,
		AppPath:         appPath,
		SkippedSymlinks: skipped,
		Sources:         sources,
	}, nil
}

// resolveRoots normalizes the input directory and selects the directory that
// should actually be packaged (promoting to the workspace root when the input
// participates in a JS workspace that uses `workspace:` deps). Promotion is
// suppressed when opts.DisableWorkspacePromotion is set.
func resolveRoots(rootDir string, opts PackageOptions) (packagingRoot, appPath string, err error) {
	selectedRoot, err := filepath.Abs(strings.TrimSpace(rootDir))
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve frontend directory: %w", err)
	}
	info, err := os.Stat(selectedRoot)
	if err != nil {
		return "", "", fmt.Errorf("failed to read frontend directory: %w", err)
	}
	if !info.IsDir() {
		return "", "", errors.New("frontend path must be a directory")
	}
	if opts.DisableWorkspacePromotion {
		return selectedRoot, "", nil
	}
	return resolvePackagingRoot(selectedRoot)
}

// archivePaths maps files to their archive paths under root, leaving out the
// ones outside it.
func archivePaths(root string, files []string) (map[string]bool, error) {
	paths := make(map[string]bool, len(files))
	for _, file := range files {
		abs, err := filepath.Abs(file)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve %s: %w", file, err)
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		paths[filepath.ToSlash(rel)] = true
	}
	return paths, nil
}

// buildArchive writes the project tree and optional app-path marker into a
// gzip-compressed tar archive. It returns the raw bytes, any archive-relative
// paths whose symlinks were dropped, and the files the archive holds.
func buildArchive(packagingRoot, appPath string, matcher *ignore.Matcher, excluded map[string]bool) ([]byte, []string, []string, error) {
	buf := new(bytes.Buffer)
	gz := gzip.NewWriter(buf)
	tw := tar.NewWriter(gz)

	skipped, sources, err := writeProjectTree(tw, packagingRoot, matcher, excluded)
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return nil, nil, nil, err
	}
	if err := writeAppPathMarker(tw, appPath); err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return nil, nil, nil, err
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return nil, nil, nil, fmt.Errorf("failed to finalize tar archive: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to finalize gzip archive: %w", err)
	}
	return buf.Bytes(), skipped, sources, nil
}

// writeProjectTree walks root and writes every regular file that neither
// matcher nor excluded leaves out into the tar writer. It enforces
// maxFrontendPackageFiles so a runaway tree (missing .gitignore, stray cache
// directory) cannot OOM the CLI, and reports any symlinks it had to drop so
// callers can warn the user, and the files it wrote.
func writeProjectTree(tw *tar.Writer, root string, matcher *ignore.Matcher, excluded map[string]bool) ([]string, []string, error) {
	fileCount := 0
	var skippedSymlinks, sources []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}

		relPath, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		if relPath == "" || relPath == "." {
			return nil
		}
		if matcher.ShouldIgnore(relPath, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if excluded[relPath] {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			if info.Mode()&os.ModeSymlink != 0 {
				skippedSymlinks = append(skippedSymlinks, relPath)
			}
			return nil
		}
		fileCount++
		if fileCount > maxFrontendPackageFiles {
			return fmt.Errorf("too many files (limit: %d)", maxFrontendPackageFiles)
		}
		sources = append(sources, path)
		return writeFileEntry(tw, path, relPath, info)
	})
	if err != nil {
		return nil, nil, err
	}
	return skippedSymlinks, sources, nil
}

// writeFileEntry writes one regular file at path under the archive name
// relPath into the tar writer.
func writeFileEntry(tw *tar.Writer, path, relPath string, info os.FileInfo) error {
	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = relPath
	header.Uname = ""
	header.Gname = ""
	header.Uid = 0
	header.Gid = 0

	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(tw, f)
	if closeErr := f.Close(); closeErr != nil && copyErr == nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	return copyErr
}

// checkAppPathMarkerCollision returns an error when the user's own tree
// already contains the marker file that writeAppPathMarker would emit and the
// matcher would not have excluded it from the archive. Writing a duplicate
// entry would silently overwrite the user's file when unpacked.
func checkAppPathMarkerCollision(packagingRoot, appPath string, matcher *ignore.Matcher) error {
	if appPath == "" {
		return nil
	}
	markerPath := filepath.Join(packagingRoot, filepath.FromSlash(frontendAppPathMarker))
	if _, err := os.Stat(markerPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", markerPath, err)
	}
	if matcher.ShouldIgnore(frontendAppPathMarker, false) {
		return nil
	}
	return fmt.Errorf("workspace already contains %s; rename it or pass --app-root to package this directory without the marker", frontendAppPathMarker)
}

// writeAppPathMarker adds a marker file naming the app subdirectory so the
// server build knows which workspace member to build. It is a no-op when
// packaging was not promoted to a workspace root.
func writeAppPathMarker(tw *tar.Writer, appPath string) error {
	if appPath == "" {
		return nil
	}
	payload := []byte(appPath + "\n")
	header := &tar.Header{
		Name: frontendAppPathMarker,
		Mode: 0o644,
		Size: int64(len(payload)),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err := tw.Write(payload)
	return err
}
