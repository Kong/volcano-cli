// Package deploycommittest fakes the git calls deploycommit makes, for
// command tests that deploy a project.
package deploycommittest

import (
	"context"
	"crypto/sha1" //nolint:gosec // git names a blob by its SHA-1
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// SHA is the commit the fake checkout has at HEAD.
const SHA = "0123456789abcdef0123456789abcdef01234567"

// Commit records every regular file under root as HEAD of a checkout whose
// origin is remote, and returns a runner that answers deploycommit's git
// calls for that checkout. A file changed after Commit reads as uncommitted.
func Commit(t *testing.T, remote, root string) cliruntime.CommandRunner {
	t.Helper()
	files, err := os.OpenRoot(root)
	require.NoError(t, err)
	defer func() { _ = files.Close() }()
	var tree strings.Builder
	err = fs.WalkDir(files.FS(), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		content, err := fs.ReadFile(files.FS(), path)
		if err != nil {
			return err
		}
		blob := sha1.Sum(append(fmt.Appendf(nil, "blob %d\x00", len(content)), content...)) //nolint:gosec // see the import
		fmt.Fprintf(&tree, "100644 blob %x\t%s\x00", blob, path)
		return nil
	})
	require.NoError(t, err)

	return cliruntime.CommandRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch strings.Join(args[2:], " ") {
		case "remote get-url origin":
			return []byte(remote + "\n"), nil
		case "rev-parse HEAD":
			return []byte(SHA + "\n"), nil
		case "ls-tree -r -z --full-tree HEAD":
			return []byte(tree.String()), nil
		}
		return nil, errors.New("unexpected git call: " + strings.Join(args, " "))
	})
}
