package function

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/Kong/volcano-cli/internal/ignore"
)

const maxPackageFiles = 10000

func readDirectoryWithMatcher(rootPath, matchRoot string, matcher *ignore.Matcher) (map[string][]byte, error) {
	files := map[string][]byte{}
	fileCount := 0
	if matchRoot == "" {
		matchRoot = rootPath
	}

	err := filepath.WalkDir(rootPath, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == rootPath {
			return nil
		}

		relPath, err := filepath.Rel(rootPath, path)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		matchPath, err := filepath.Rel(matchRoot, path)
		if err != nil {
			return err
		}
		matchPath = filepath.ToSlash(matchPath)
		projectMatchPath := matchPath
		if matcher != nil {
			projectMatchPath, err = matcher.ProjectRelPath(path)
			if err != nil {
				return err
			}
		}
		if matcher != nil && matcher.ShouldIgnoreWith(matchPath, projectMatchPath, entry.IsDir()) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}

		fileCount++
		if fileCount > maxPackageFiles {
			return fmt.Errorf("too many files (limit: %d)", maxPackageFiles)
		}
		content, err := os.ReadFile(path) //nolint:gosec // path comes from filepath.Walk over the user-selected function source tree
		if err != nil {
			return err
		}
		files[relPath] = content
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
