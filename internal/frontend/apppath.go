// Package frontend builds and packages frontend project archives for upload.
package frontend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// NormalizeAppRoot validates and normalizes a user-supplied --app-root flag.
// Empty or "." values become "", meaning the archive root is the app root.
func NormalizeAppRoot(raw string) (string, error) {
	appRoot := strings.TrimSpace(raw)
	if appRoot == "" || appRoot == "." {
		return "", nil
	}
	for _, r := range appRoot {
		if unicode.IsSpace(r) {
			return "", errors.New("--app-root must not contain whitespace characters")
		}
		if !unicode.IsPrint(r) {
			return "", errors.New("--app-root must not contain control or non-printable characters")
		}
	}
	if strings.Contains(appRoot, `\`) {
		return "", errors.New("--app-root must use POSIX '/' path separators")
	}
	if filepath.IsAbs(appRoot) || strings.HasPrefix(appRoot, "/") {
		return "", errors.New("--app-root must be a relative path")
	}
	parts := strings.SplitSeq(appRoot, "/")
	for part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("--app-root must be a normalized relative path")
		}
	}
	cleaned := filepath.ToSlash(filepath.Clean(appRoot))
	if cleaned != appRoot || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", errors.New("--app-root must be a normalized relative path")
	}
	return cleaned, nil
}

// ValidateAppRootExists verifies that --app-root resolves to a directory under
// the packaged archive root.
func ValidateAppRootExists(archiveRoot, appRoot string) error {
	if strings.TrimSpace(appRoot) == "" {
		return nil
	}
	root := strings.TrimSpace(archiveRoot)
	if root == "" {
		return errors.New("--path is required when --app-root is set")
	}
	target := filepath.Join(root, filepath.FromSlash(appRoot))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("--app-root must stay within --path")
	}
	info, err := os.Stat(target)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("--app-root %q does not exist under --path %s", appRoot, root)
		}
		return fmt.Errorf("failed to inspect --app-root %q: %w", appRoot, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("--app-root %q must point to a directory", appRoot)
	}
	return nil
}
