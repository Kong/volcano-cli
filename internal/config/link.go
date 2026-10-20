package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/symlink"
	"github.com/Kong/volcano-cli/internal/theme"
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

// ErrHomeLink refuses a link in the home directory, whose .volcano directory
// holds the CLI's own config.
var ErrHomeLink = errors.New("can't link your home directory, because its .volcano directory holds the CLI's config. Use a directory under it")

// FindLink looks for .volcano/project.json in start and its parents. The walk
// stops after the first directory holding a .git entry, and below the home
// directory, so a repository never picks up a link from a directory above it.
// It returns nil when no link is found.
func FindLink(start string) (*Link, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return nil, err
	}
	for {
		// ~/.volcano is the CLI's config directory, not a link.
		if IsHomeDir(dir) {
			return nil, nil //nolint:nilnil // an unlinked directory is an outcome, not a failure
		}
		link, err := readLink(dir)
		if link != nil || err != nil {
			return link, err
		}
		if exists(filepath.Join(dir, ".git")) {
			return nil, nil //nolint:nilnil // an unlinked directory is an outcome, not a failure
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, nil //nolint:nilnil // an unlinked directory is an outcome, not a failure
		}
		dir = parent
	}
}

// IsHomeDir reports whether dir, an absolute path, is the home directory.
func IsHomeDir(dir string) bool {
	home, err := os.UserHomeDir()
	return err == nil && home != "" && filepath.Clean(dir) == filepath.Clean(home)
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
// without a valid project ID.
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

// maxLinkSize is far above a real link, which is a few lines of JSON.
const maxLinkSize = 64 << 10

func readLink(dir string) (*Link, error) {
	path := filepath.Join(dir, LinkRelPath)
	info, err := os.Lstat(path)
	// ENOTDIR: dir holds a file named .volcano, so no link.
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return nil, nil //nolint:nilnil // no link here is an outcome, not a failure
	}
	if err != nil {
		return nil, &LinkError{Dir: dir, Err: fmt.Errorf("failed to read project link %s: %w", path, err)}
	}
	// A cloned repository could commit a symlink to /dev/zero or a FIFO, which
	// would hang every command run in it.
	if !info.Mode().IsRegular() {
		return nil, &LinkError{Dir: dir, Err: fmt.Errorf("project link %s is not a regular file", path)}
	}
	data, err := readLimited(path, maxLinkSize)
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
	// The ID ends up in printed commands, so a link committed to a cloned
	// repository must not be able to put shell code there.
	id, err := uuid.Parse(strings.TrimSpace(file.ProjectID))
	if err != nil {
		return nil, &LinkError{Dir: dir, Err: fmt.Errorf("project link %s has a project_id that isn't a project ID: %q", path, file.ProjectID)}
	}
	// The canonical form is the one the API returns, so every spelling the
	// parser accepts matches the same project. The name is printed, so it
	// can't carry terminal escape sequences either.
	return &Link{
		Dir:     dir,
		Project: ProjectConfig{ID: id.String(), Name: theme.StripControl(file.ProjectName)},
	}, nil
}

// readLimited reads path, failing when it is larger than limit bytes.
func readLimited(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return data, nil
}

// WriteLink links dir to project by writing dir/.volcano/project.json. It
// refuses to write through a symlink, and in the home directory.
func WriteLink(dir string, project ProjectConfig) (*Link, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if id, err := uuid.Parse(strings.TrimSpace(project.ID)); err == nil {
		project.ID = id.String()
	}
	if IsHomeDir(abs) {
		return nil, ErrHomeLink
	}
	link := &Link{Dir: abs, Project: project}
	if err := symlink.Refuse(abs, LinkRelPath); err != nil {
		return nil, err
	}
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
