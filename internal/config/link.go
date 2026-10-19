package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	linkDirName  = ".volcano"
	linkFileName = "project.json"
	linkDirMode  = 0o755
	linkFileMode = 0o644
)

// LinkRelPath is where a linked directory records its project, relative to
// that directory.
var LinkRelPath = filepath.Join(linkDirName, linkFileName)

// Link is a directory's link to a cloud project, read from
// .volcano/project.json. Inside a linked directory the linked project takes
// precedence over the one `volcano use` saved globally, so two checkouts on one
// machine can point at different projects.
type Link struct {
	// Dir is the linked directory, the one holding .volcano/project.json.
	Dir     string
	Project ProjectConfig
}

// Path returns the link file's path.
func (l Link) Path() string {
	return filepath.Join(l.Dir, LinkRelPath)
}

type linkFile struct {
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name,omitempty"`
}

// FindLink looks for .volcano/project.json in start and its parents. The walk
// stops after the first directory holding a .git entry, and at the home
// directory, so a repository never picks up a link from a directory above it.
// It returns nil when no link is found.
func FindLink(start string) (*Link, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	for {
		link, err := readLink(dir)
		if link != nil || err != nil {
			return link, err
		}
		if dir == home || exists(filepath.Join(dir, ".git")) {
			return nil, nil //nolint:nilnil // an unlinked directory is an outcome, not a failure
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil //nolint:nilnil // an unlinked directory is an outcome, not a failure
		}
		dir = parent
	}
}

// FindLinkFromWorkingDir is FindLink from the current directory.
func FindLinkFromWorkingDir() (*Link, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}
	return FindLink(wd)
}

// LinkError is a project link that can't be used: unreadable, malformed, or
// without a project ID.
type LinkError struct {
	// Dir is the directory holding the link.
	Dir string
	Err error
}

func (e *LinkError) Error() string { return e.Err.Error() }

func (e *LinkError) Unwrap() error { return e.Err }

// Path returns the link file's path.
func (e *LinkError) Path() string {
	return filepath.Join(e.Dir, LinkRelPath)
}

func readLink(dir string) (*Link, error) {
	path := filepath.Join(dir, LinkRelPath)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil //nolint:nilnil // no link here is an outcome, not a failure
	}
	if err != nil {
		return nil, &LinkError{Dir: dir, Err: fmt.Errorf("failed to read project link %s: %w", path, err)}
	}
	var file linkFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, &LinkError{Dir: dir, Err: fmt.Errorf("failed to parse project link %s: %w", path, err)}
	}
	if strings.TrimSpace(file.ProjectID) == "" {
		return nil, &LinkError{Dir: dir, Err: fmt.Errorf("project link %s has no project_id", path)}
	}
	return &Link{
		Dir:     dir,
		Project: ProjectConfig{ID: strings.TrimSpace(file.ProjectID), Name: file.ProjectName},
	}, nil
}

// WriteLink links dir to project by writing dir/.volcano/project.json.
func WriteLink(dir string, project ProjectConfig) (*Link, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	link := &Link{Dir: abs, Project: project}
	if err := os.MkdirAll(filepath.Dir(link.Path()), linkDirMode); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", filepath.Dir(link.Path()), err)
	}
	data, err := json.MarshalIndent(linkFile{ProjectID: project.ID, ProjectName: project.Name}, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(link.Path(), append(data, '\n'), linkFileMode); err != nil {
		return nil, fmt.Errorf("failed to write project link %s: %w", link.Path(), err)
	}
	return link, nil
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
