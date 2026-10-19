// Package symlink keeps the CLI from writing through a symlink.
package symlink

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Refuse returns an error when the file at rel inside root, or a directory
// between root and it, is a symlink. Writing through one would change a file
// outside root, such as ~/.volcano/config.json named by a symlink in a cloned
// repository. root and the directories above it may be symlinks: the user
// chose that directory. Call Refuse before creating directories or writing.
func Refuse(root, rel string) error {
	path := filepath.Join(root, rel)
	current := root
	for part := range strings.SplitSeq(filepath.Clean(rel), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			// Missing parts are created by the write, and any other error
			// is reported by it.
			return nil
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			continue
		}
		if current == path {
			return fmt.Errorf("%s is a symlink, and the CLI doesn't write through one. Remove it and try again", path)
		}
		return fmt.Errorf("%s is inside %s, a symlink, and the CLI doesn't write through one. "+
			"Remove the symlink and try again", path, current)
	}
	return nil
}
