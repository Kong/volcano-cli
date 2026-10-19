package envfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	dirMode fs.FileMode = 0o755
	// fileMode is owner-only: env files hold keys.
	fileMode fs.FileMode = 0o600
)

// Var is one KEY=value line Upsert writes.
type Var struct {
	Key, Value string
}

// Upsert sets vars in the env file at path, keeping every other line and
// comment as it is. A key already in the file is updated in place, so a rerun
// never adds a second copy. Later copies of a key are dropped: the last one
// would win when the file is read. A new file starts with header.
func Upsert(path string, vars []Var, header ...string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	var lines []string
	if len(data) > 0 {
		lines = strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	} else {
		lines = append(lines, header...)
	}
	for _, v := range vars {
		found := false
		kept := lines[:0]
		for _, line := range lines {
			if Key(line) != v.Key {
				kept = append(kept, line)
				continue
			}
			if !found {
				kept = append(kept, v.Key+"="+v.Value)
				found = true
			}
		}
		lines = kept
		if !found {
			lines = append(lines, v.Key+"="+v.Value)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return fmt.Errorf("failed to create %s: %w", filepath.Dir(path), err)
	}
	//nolint:gosec // path is an env file in a directory the user linked to a project
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), fileMode); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

// HasKey reports whether the env file at path sets any of keys. A missing
// file sets none.
func HasKey(path string, keys ...string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if key := Key(line); key != "" && slices.Contains(keys, key) {
			return true, nil
		}
	}
	return false, nil
}

// Key returns the key a KEY=value line sets, or "" for anything else.
func Key(line string) string {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return ""
	}
	line = strings.TrimPrefix(line, "export ")
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	return strings.TrimSpace(key)
}
