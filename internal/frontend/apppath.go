// Package frontend builds and packages frontend project archives for upload.
package frontend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// HasBuildPackage reports whether the app in appDir has a package.json that
// builds it: its own, or the nearest one above it inside archiveRoot whose
// build script runs "next build <appDir>". volcano init nextjs creates the
// second layout, and Volcano's builders accept both.
func HasBuildPackage(archiveRoot, appDir string) (bool, error) {
	root, err := filepath.Abs(archiveRoot)
	if err != nil {
		return false, err
	}
	app, err := filepath.Abs(appDir)
	if err != nil {
		return false, err
	}
	if found, err := isFile(filepath.Join(app, "package.json")); found || err != nil {
		return found, err
	}
	for dir := app; dir != root; {
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, nil
		}
		dir = parent
		data, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("failed to read package.json at %s: %w", dir, err)
		}
		built, ok := nextBuildDir(data)
		return ok && filepath.Join(dir, filepath.FromSlash(built)) == app, nil
	}
	return false, nil
}

// nextBuildDir returns the directory argument of the "next build" command in
// a package.json build script.
func nextBuildDir(packageJSON []byte) (string, bool) {
	var pkg struct {
		Scripts map[string]any `json:"scripts"`
	}
	if json.Unmarshal(packageJSON, &pkg) != nil {
		return "", false
	}
	build, _ := pkg.Scripts["build"].(string)
	idx := strings.LastIndex(build, "next build")
	if idx < 0 {
		return "", false
	}
	segment := build[idx+len("next build"):]
	if end := strings.IndexAny(segment, ";&|"); end >= 0 {
		segment = segment[:end]
	}
	for token := range strings.FieldsSeq(segment) {
		if !strings.HasPrefix(token, "-") {
			return token, true
		}
	}
	return "", false
}

func isFile(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return !info.IsDir(), nil
}
