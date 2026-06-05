package frontend

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// maxWorkspaceWalkSteps caps how far above the selected directory the
// workspace search may travel. Real monorepos rarely nest deeper than this,
// and the cap stops a stray ancestor declaration from re-rooting packaging.
const maxWorkspaceWalkSteps = 8

// resolvePackagingRoot returns the directory that should be packaged for
// selectedRoot, plus the relative app-path marker stored in the archive when
// auto-detection promotes packaging up to a workspace root. Promotion only
// happens when the workspace search stays inside the surrounding git repo and
// when selectedRoot actually matches one of the workspace's globs.
func resolvePackagingRoot(selectedRoot string) (string, string, error) {
	if !projectUsesWorkspaceProtocol(selectedRoot) {
		return selectedRoot, "", nil
	}
	workspaceRoot, globs, ok := findWorkspaceRoot(selectedRoot)
	if !ok || workspaceRoot == selectedRoot {
		return selectedRoot, "", nil
	}
	relPath, err := filepath.Rel(workspaceRoot, selectedRoot)
	if err != nil {
		return "", "", fmt.Errorf("failed to resolve frontend app path: %w", err)
	}
	relPath = filepath.ToSlash(relPath)
	if relPath == "." || relPath == "" || strings.HasPrefix(relPath, "../") {
		return selectedRoot, "", nil
	}
	if !workspaceContains(globs, relPath) {
		return selectedRoot, "", nil
	}
	return workspaceRoot, relPath, nil
}

// findWorkspaceRoot walks ancestors of start looking for a workspace
// declaration, bounded by the surrounding git repo and by
// maxWorkspaceWalkSteps. Without a git ancestor we refuse to walk at all — a
// stray ancestor declaration (e.g. a leftover workspaces field in $HOME) must
// not silently re-root packaging. It returns the discovered root, its
// declared globs, and whether the search succeeded.
func findWorkspaceRoot(start string) (string, []string, bool) {
	gitRoot := findGitRoot(start)
	if gitRoot == "" {
		return "", nil, false
	}
	current := start
	for steps := 0; steps <= maxWorkspaceWalkSteps; steps++ {
		if globs, ok := readWorkspaceGlobs(current); ok {
			return current, globs, true
		}
		if current == gitRoot {
			return "", nil, false
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil, false
		}
		current = parent
	}
	return "", nil, false
}

// findGitRoot returns the nearest ancestor of start containing a .git entry,
// or "" if none. .git may be a directory (regular checkout) or a file (worktree).
func findGitRoot(start string) string {
	current := start
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// readWorkspaceGlobs returns the workspace globs declared at dir, preferring
// pnpm-workspace.yaml over package.json.
func readWorkspaceGlobs(dir string) ([]string, bool) {
	if globs, ok := readPnpmWorkspaceGlobs(filepath.Join(dir, "pnpm-workspace.yaml")); ok {
		return globs, true
	}
	pkg, ok := readPackageJSON(filepath.Join(dir, "package.json"))
	if !ok {
		return nil, false
	}
	raw, hasWorkspaces := pkg["workspaces"]
	if !hasWorkspaces {
		return nil, false
	}
	globs := decodeWorkspacesField(raw)
	return globs, len(globs) > 0
}

func readPnpmWorkspaceGlobs(filePath string) ([]string, bool) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false
	}
	var doc struct {
		Packages []string `yaml:"packages"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, false
	}
	return doc.Packages, len(doc.Packages) > 0
}

// decodeWorkspacesField handles both the npm/yarn array form
// (`"workspaces": ["apps/*"]`) and the yarn object form
// (`"workspaces": {"packages": ["apps/*"]}`).
func decodeWorkspacesField(raw any) []string {
	switch v := raw.(type) {
	case []any:
		return collectStrings(v)
	case map[string]any:
		if pkgs, ok := v["packages"].([]any); ok {
			return collectStrings(pkgs)
		}
	}
	return nil
}

func collectStrings(items []any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// workspaceContains reports whether relPath (POSIX, relative to the workspace
// root) matches the declared workspace globs. Negation patterns (prefixed `!`)
// remove an earlier positive match.
func workspaceContains(globs []string, relPath string) bool {
	matched := false
	for _, glob := range globs {
		if after, ok := strings.CutPrefix(glob, "!"); ok {
			if matchWorkspaceGlob(after, relPath) {
				matched = false
			}
			continue
		}
		if matchWorkspaceGlob(glob, relPath) {
			matched = true
		}
	}
	return matched
}

// matchWorkspaceGlob reports whether relPath matches a single workspace
// glob. pnpm/Yarn/npm workspace declarations commonly use `**` to match
// across path separators (e.g. `packages/**`, `apps/**/web`), which
// path.Match does not support. matchWorkspaceGlob extends path.Match's
// per-segment semantics with `**` matching zero or more path segments.
func matchWorkspaceGlob(glob, relPath string) bool {
	cleaned := path.Clean(strings.TrimSuffix(glob, "/"))
	if cleaned == "." || cleaned == "" {
		return false
	}
	return matchGlobSegments(strings.Split(cleaned, "/"), strings.Split(relPath, "/"))
}

// matchGlobSegments matches POSIX-split path segments against pattern
// segments where the pattern segment `**` matches zero or more path
// segments. Other segments are matched with path.Match (so `*` still
// matches within a single segment but does not cross `/`).
func matchGlobSegments(pattern, name []string) bool {
	for len(pattern) > 0 {
		head := pattern[0]
		if head == "**" {
			rest := pattern[1:]
			if len(rest) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchGlobSegments(rest, name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		ok, _ := path.Match(head, name[0])
		if !ok {
			return false
		}
		pattern = pattern[1:]
		name = name[1:]
	}
	return len(name) == 0
}

func projectUsesWorkspaceProtocol(dir string) bool {
	pkg, ok := readPackageJSON(filepath.Join(dir, "package.json"))
	if !ok {
		return false
	}
	for _, field := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		rawMap, ok := pkg[field]
		if !ok {
			continue
		}
		depsMap, ok := rawMap.(map[string]any)
		if !ok {
			continue
		}
		for _, rawVersion := range depsMap {
			version, ok := rawVersion.(string)
			if !ok {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(version), "workspace:") {
				return true
			}
		}
	}
	return false
}

func readPackageJSON(filePath string) (map[string]any, bool) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, false
	}
	payload := map[string]any{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, false
	}
	return payload, true
}
