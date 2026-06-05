// Package ignore decides which files to skip when packaging a project tree.
// It applies a built-in list of always-ignored patterns, any extras supplied
// by the caller, and the project's .gitignore (when present at the root).
package ignore

import (
	"fmt"
	"os"
	"path/filepath"

	gitignore "github.com/sabhiram/go-gitignore"
)

// defaultPatterns are always ignored, regardless of project .gitignore.
func defaultPatterns() []string {
	return []string{
		".git",
		".gitignore",
		".DS_Store",
		"__pycache__",
		"*.pyc",
		"*.pyo",
		".pytest_cache",
		".mypy_cache",
		".ruff_cache",
		".bundle",
		"*.rbc",
		"*.log",
		".vscode",
		".idea",
		"*.swp",
		"*.swo",
	}
}

// Matcher decides whether a path should be excluded from an archive.
type Matcher struct {
	root      string
	patterns  []string
	gitIgnore *gitignore.GitIgnore
}

// NewProjectMatcher returns a Matcher rooted at root. The default ignore
// patterns are always applied. extraPatterns are layered on top, and the
// root .gitignore (if present) is consulted last.
func NewProjectMatcher(root string, extraPatterns ...string) (*Matcher, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve ignore root: %w", err)
	}

	patterns := append([]string{}, defaultPatterns()...)
	patterns = append(patterns, extraPatterns...)
	matcher := &Matcher{
		root:     absRoot,
		patterns: patterns,
	}

	ignorePath := filepath.Join(absRoot, ".gitignore")
	if _, err := os.Stat(ignorePath); err != nil {
		if os.IsNotExist(err) {
			return matcher, nil
		}
		return nil, fmt.Errorf("failed to read .gitignore: %w", err)
	}

	compiled, err := gitignore.CompileIgnoreFile(ignorePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse .gitignore: %w", err)
	}
	matcher.gitIgnore = compiled
	return matcher, nil
}

// ShouldIgnore reports whether relPath should be skipped. relPath is used for
// both pattern matching and gitignore lookup. Use ShouldIgnoreWith when those
// paths differ (for example, when archiving shared files from outside the
// walked tree).
func (m *Matcher) ShouldIgnore(relPath string, isDir bool) bool {
	return m.ShouldIgnoreWith(relPath, relPath, isDir)
}

// ShouldIgnoreWith reports whether a path should be skipped, allowing the
// pattern-matching path and the project-relative path used for .gitignore
// lookup to differ.
func (m *Matcher) ShouldIgnoreWith(patternRelPath, projectRelPath string, isDir bool) bool {
	if m == nil {
		return false
	}
	if matchAny(patternRelPath, m.patterns) {
		return true
	}
	if m.gitIgnore == nil {
		return false
	}

	cleanPath := filepath.ToSlash(projectRelPath)
	if m.gitIgnore.MatchesPath(cleanPath) {
		return true
	}
	return isDir && m.gitIgnore.MatchesPath(cleanPath+"/")
}

// ProjectRelPath returns absPath relative to the matcher root using forward
// slashes. It is intended for callers that walk files outside the immediate
// project root and need a path to feed back into ShouldIgnoreWith.
func (m *Matcher) ProjectRelPath(absPath string) (string, error) {
	if m == nil || m.root == "" {
		return filepath.ToSlash(absPath), nil
	}
	resolved, err := filepath.Abs(absPath)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(m.root, resolved)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func matchAny(relPath string, patterns []string) bool {
	cleanPath := filepath.ToSlash(relPath)
	segments := splitPathSegments(cleanPath)
	for _, pattern := range patterns {
		if match, _ := filepath.Match(pattern, filepath.Base(cleanPath)); match {
			return true
		}
		if match, _ := filepath.Match(pattern, cleanPath); match {
			return true
		}
		for _, segment := range segments {
			if match, _ := filepath.Match(pattern, segment); match {
				return true
			}
		}
	}
	return false
}

func splitPathSegments(relPath string) []string {
	if relPath == "" {
		return nil
	}
	parts := make([]string, 0, 8)
	start := 0
	for i := 0; i <= len(relPath); i++ {
		if i < len(relPath) && relPath[i] != '/' {
			continue
		}
		if i > start {
			parts = append(parts, relPath[start:i])
		}
		start = i + 1
	}
	return parts
}
