package frontend

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SourceLayoutPath is where a project lists its frontends. Volcano writes it on
// source export and reads it when it deploys a connected repository, so a
// project deploys the same frontends from the CLI and from git.
const SourceLayoutPath = ".volcano/source-layout.json"

const maxLayoutNameLen = 63

// SourceLayout lists a project's frontends relative to the project root.
type SourceLayout struct {
	Version   int              `json:"version"`
	Frontends []LayoutFrontend `json:"frontends"`
}

// LayoutFrontend is one frontend: the directory archived for it and the app
// inside that directory.
type LayoutFrontend struct {
	Name string `json:"name"`
	// Workspace is the archived directory. Omitted means frontends/<name>;
	// empty means the project root.
	Workspace *string `json:"workspace,omitempty"`
	AppRoot   string  `json:"app_root"`
}

// Dir returns the directory archived for the frontend, relative to the project
// root, in POSIX form. The project root is "".
func (f LayoutFrontend) Dir() string {
	if f.Workspace != nil {
		return *f.Workspace
	}
	return "frontends/" + f.Name
}

// ReadSourceLayout reads the layout file under projectRoot. found is false when
// the project has none.
func ReadSourceLayout(projectRoot string) (layout SourceLayout, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(projectRoot, filepath.FromSlash(SourceLayoutPath)))
	if errors.Is(err, fs.ErrNotExist) {
		return SourceLayout{}, false, nil
	}
	if err != nil {
		return SourceLayout{}, false, fmt.Errorf("failed to read %s: %w", SourceLayoutPath, err)
	}
	layout, err = ParseSourceLayout(data)
	if err != nil {
		return SourceLayout{}, false, err
	}
	return layout, true, nil
}

// ParseSourceLayout decodes and validates a layout file with the rules the
// server applies to one in a connected repository.
func ParseSourceLayout(data []byte) (SourceLayout, error) {
	var layout SourceLayout
	if err := json.Unmarshal(data, &layout); err != nil {
		return SourceLayout{}, fmt.Errorf("%s: %w", SourceLayoutPath, err)
	}
	if layout.Version != 1 {
		return SourceLayout{}, fmt.Errorf("%s: unsupported version %d (expected 1)", SourceLayoutPath, layout.Version)
	}
	seen := make(map[string]bool, len(layout.Frontends))
	for _, fe := range layout.Frontends {
		if err := validateLayoutFrontend(fe); err != nil {
			return SourceLayout{}, fmt.Errorf("%s: frontend %q: %w", SourceLayoutPath, fe.Name, err)
		}
		if seen[fe.Name] {
			return SourceLayout{}, fmt.Errorf("%s: frontend %q is listed twice", SourceLayoutPath, fe.Name)
		}
		seen[fe.Name] = true
	}
	return layout, nil
}

func validateLayoutFrontend(fe LayoutFrontend) error {
	if !validLayoutName(fe.Name) {
		return errors.New("name must be 1 to 63 lowercase letters, digits, or inner hyphens")
	}
	if !cleanLayoutPath(fe.Dir()) {
		return fmt.Errorf("workspace %q must be a normalized path inside the project", fe.Dir())
	}
	if !cleanLayoutPath(fe.AppRoot) {
		return fmt.Errorf("app_root %q must be a normalized path inside the workspace", fe.AppRoot)
	}
	return nil
}

func validLayoutName(name string) bool {
	if name == "" || len(name) > maxLayoutNameLen {
		return false
	}
	for i := range len(name) {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-' && i != 0 && i != len(name)-1:
		default:
			return false
		}
	}
	return true
}

// cleanLayoutPath accepts "" or a relative POSIX path already in clean form
// that stays inside its base.
func cleanLayoutPath(value string) bool {
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "/") || strings.Contains(value, `\`) || strings.TrimSpace(value) != value {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}
