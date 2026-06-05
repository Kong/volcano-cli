// Package projectinit creates the on-disk Volcano project scaffold.
package projectinit

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	fileMode fs.FileMode = 0o644
	dirMode  fs.FileMode = 0o755

	baseStarter    = "base"
	defaultStarter = "javascript"
	startersRoot   = "starters"

	nestedEnvPath    = "volcano/volcano.env"
	rootEnvPath      = "volcano.env"
	nestedConfigPath = "volcano/volcano-config.yaml"
	rootConfigPath   = "volcano-config.yaml"
)

//go:embed all:starters
var startersFS embed.FS

// Result describes the filesystem changes made by one init run.
type Result struct {
	created     []string
	unchanged   []string
	overwritten []string
}

// Created returns paths created by init.
func (r *Result) Created() []string {
	return slices.Clone(r.created)
}

// Unchanged returns existing paths that already matched the starter.
func (r *Result) Unchanged() []string {
	return slices.Clone(r.unchanged)
}

// Overwritten returns paths replaced because force mode was enabled.
func (r *Result) Overwritten() []string {
	return slices.Clone(r.overwritten)
}

type conflict struct {
	Path   string
	Reason string
}

type conflictError struct {
	conflicts []conflict
}

func (e *conflictError) Error() string {
	if len(e.conflicts) == 1 {
		conflict := e.conflicts[0]
		return fmt.Sprintf("project init conflict: %s %s", conflict.Path, conflict.Reason)
	}
	return fmt.Sprintf("project init found %d conflicts", len(e.conflicts))
}

// ConflictMessages returns formatted conflict details from an init error.
func ConflictMessages(err error) ([]string, bool) {
	var conflictErr *conflictError
	if !errors.As(err, &conflictErr) {
		return nil, false
	}

	messages := make([]string, 0, len(conflictErr.conflicts))
	for _, conflict := range conflictErr.conflicts {
		messages = append(messages, fmt.Sprintf("%s (%s)", conflict.Path, conflict.Reason))
	}
	return messages, true
}

// ConflictsCanBeForced reports whether --force can resolve every conflict in err.
func ConflictsCanBeForced(err error) bool {
	var conflictErr *conflictError
	if !errors.As(err, &conflictErr) {
		return false
	}

	for _, conflict := range conflictErr.conflicts {
		if conflict.Reason != "has different content" {
			return false
		}
	}
	return len(conflictErr.conflicts) > 0
}

type plannedFile struct {
	path    string
	content []byte
}

type plan struct {
	root   string
	dirs   []string
	dirSet map[string]struct{}
	files  []plannedFile
	result Result
}

// Run creates the Volcano project scaffold in the current directory.
func Run(force bool) (*Result, error) {
	return RunStarter("", force)
}

// RunStarter creates the base scaffold plus an optional starter overlay.
func RunStarter(starterName string, force bool) (*Result, error) {
	return run("", starterName, force)
}

func run(rootDir, starterName string, force bool) (*Result, error) {
	planned, err := buildPlan(rootDir, starterName, force)
	if err != nil {
		return nil, err
	}
	if err := planned.apply(); err != nil {
		return nil, err
	}
	return &planned.result, nil
}

func buildPlan(rootDir, starterName string, force bool) (*plan, error) {
	root, err := resolveRootDir(rootDir)
	if err != nil {
		return nil, err
	}

	planned := &plan{root: root, dirSet: make(map[string]struct{})}
	starterNames := []string{baseStarter}
	starterName = strings.TrimSpace(starterName)
	if starterName == "" {
		starterName = defaultStarter
	}
	if !validStarterName(starterName) {
		return nil, fmt.Errorf("invalid starter name %q", starterName)
	}
	if !starterExists(starterName) {
		return nil, fmt.Errorf("unknown starter %q", starterName)
	}
	if starterName != baseStarter {
		starterNames = append(starterNames, starterName)
	}
	for _, starterName := range starterNames {
		starter, err := openStarter(starterName)
		if err != nil {
			return nil, err
		}
		if err := planned.walkStarter(starter, force); err != nil {
			return nil, err
		}
	}
	return planned, nil
}

func validStarterName(name string) bool {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, ".") {
		return false
	}
	for part := range strings.SplitSeq(name, "-") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func starterExists(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	info, err := fs.Stat(startersFS, path.Join(startersRoot, name))
	return err == nil && info.IsDir()
}

func resolveRootDir(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
		root = cwd
	}

	resolved, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project directory: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("failed to inspect project directory %s: %w", resolved, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project directory %s is not a directory", resolved)
	}
	return resolved, nil
}

func openStarter(name string) (fs.FS, error) {
	starter, err := fs.Sub(startersFS, path.Join(startersRoot, name))
	if err != nil {
		return nil, fmt.Errorf("failed to load starter %s: %w", name, err)
	}
	return starter, nil
}

func (p *plan) walkStarter(starter fs.FS, force bool) error {
	var conflicts []conflict
	err := fs.WalkDir(starter, ".", func(relativePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if relativePath == "." {
			return nil
		}
		if entry.IsDir() {
			conflict, hasConflict, err := p.planDirectory(relativePath)
			if err != nil {
				return err
			}
			if hasConflict {
				conflicts = append(conflicts, conflict)
				return fs.SkipDir
			}
			return nil
		}

		conflict, hasConflict, err := p.planFile(starter, relativePath, force)
		if err != nil {
			return err
		}
		if hasConflict {
			conflicts = append(conflicts, conflict)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(conflicts) > 0 {
		return &conflictError{conflicts: conflicts}
	}
	return nil
}

func (p *plan) planDirectory(relativePath string) (conflict, bool, error) {
	if _, ok := p.dirSet[relativePath]; ok {
		return conflict{}, false, nil
	}
	info, err := os.Lstat(p.targetPath(relativePath))
	if err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return conflict{Path: relativePath, Reason: "exists and is not a directory"}, true, nil
		}
		p.dirSet[relativePath] = struct{}{}
		p.result.unchanged = append(p.result.unchanged, relativePath)
		return conflict{}, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return conflict{}, false, fmt.Errorf("failed to inspect %s: %w", relativePath, err)
	}
	p.dirs = append(p.dirs, relativePath)
	p.dirSet[relativePath] = struct{}{}
	p.result.created = append(p.result.created, relativePath)
	return conflict{}, false, nil
}

func (p *plan) planFile(starter fs.FS, relativePath string, force bool) (conflict, bool, error) {
	shouldWrite, err := p.shouldWriteStarterFile(relativePath)
	if err != nil {
		return conflict{}, false, err
	}
	if !shouldWrite {
		return conflict{}, false, nil
	}

	content, err := fs.ReadFile(starter, relativePath)
	if err != nil {
		return conflict{}, false, fmt.Errorf("failed to read starter file %s: %w", relativePath, err)
	}

	info, err := os.Lstat(p.targetPath(relativePath))
	if err == nil {
		if info.IsDir() {
			return conflict{Path: relativePath, Reason: "exists and is a directory"}, true, nil
		}
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return conflict{Path: relativePath, Reason: "exists and is not a regular file"}, true, nil
		}

		current, err := os.ReadFile(p.targetPath(relativePath))
		if err != nil {
			return conflict{}, false, fmt.Errorf("failed to read %s: %w", relativePath, err)
		}
		if bytes.Equal(current, content) {
			p.result.unchanged = append(p.result.unchanged, relativePath)
			return conflict{}, false, nil
		}
		if !force {
			return conflict{Path: relativePath, Reason: "has different content"}, true, nil
		}

		p.files = append(p.files, plannedFile{path: relativePath, content: content})
		p.result.overwritten = append(p.result.overwritten, relativePath)
		return conflict{}, false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return conflict{}, false, fmt.Errorf("failed to inspect %s: %w", relativePath, err)
	}

	p.files = append(p.files, plannedFile{path: relativePath, content: content})
	p.result.created = append(p.result.created, relativePath)
	return conflict{}, false, nil
}

func (p *plan) shouldWriteStarterFile(relativePath string) (bool, error) {
	switch relativePath {
	case nestedEnvPath:
		return p.shouldWriteNestedStandardFile(nestedEnvPath, rootEnvPath)
	case nestedConfigPath:
		return p.shouldWriteNestedStandardFile(nestedConfigPath, rootConfigPath)
	default:
		return true, nil
	}
}

func (p *plan) shouldWriteNestedStandardFile(nestedPath, rootPath string) (bool, error) {
	rootInfo, err := os.Lstat(p.targetPath(rootPath))
	if err == nil {
		nestedExists, err := pathExists(p.targetPath(nestedPath))
		if err != nil {
			return false, fmt.Errorf("failed to inspect %s: %w", nestedPath, err)
		}
		if nestedExists {
			return false, ambiguousStandardFileError(nestedPath, rootPath)
		}
		if !rootInfo.Mode().IsRegular() || rootInfo.Mode()&os.ModeSymlink != 0 {
			return false, &conflictError{conflicts: []conflict{{
				Path:   rootPath,
				Reason: "exists and is not a regular file",
			}}}
		}

		p.result.unchanged = append(p.result.unchanged, rootPath)
		return false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("failed to inspect %s: %w", rootPath, err)
	}
	return true, nil
}

func pathExists(target string) (bool, error) {
	_, err := os.Lstat(target)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func ambiguousStandardFileError(nestedPath, rootPath string) error {
	switch rootPath {
	case rootEnvPath:
		return fmt.Errorf("found multiple volcano.env files: %s, %s.\nplease keep only one volcano.env file, or specify explicitly with --file", nestedPath, rootPath)
	case rootConfigPath:
		return fmt.Errorf("found multiple volcano-config.yaml files: %s, %s.\nplease keep only one volcano-config.yaml file, or specify explicitly with --file", nestedPath, rootPath)
	default:
		return fmt.Errorf("found multiple files: %s, %s", nestedPath, rootPath)
	}
}

func (p *plan) apply() error {
	for _, dir := range p.dirs {
		if err := os.MkdirAll(p.targetPath(dir), dirMode); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	for _, file := range p.files {
		if err := os.WriteFile(p.targetPath(file.path), file.content, fileMode); err != nil {
			return fmt.Errorf("failed to write %s: %w", file.path, err)
		}
	}
	return nil
}

func (p *plan) targetPath(relativePath string) string {
	return filepath.Join(p.root, filepath.FromSlash(relativePath))
}
