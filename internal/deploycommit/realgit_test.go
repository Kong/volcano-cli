package deploycommit

// These tests run the installed git, so the arguments Detect and Commit pass,
// and the blob IDs they compare, are checked against git itself rather than
// against a fake that shares the code's assumptions.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	helloPath = "volcano/functions/hello.js"
	utilPath  = "volcano/functions/_shared/util.js"
)

// newRepository makes a repository with a GitHub origin and one commit
// holding two function files and a .gitignore.
func newRepository(t *testing.T) (root, head string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, root, helloPath, "exports.handler = 1\n")
	writeFile(t, root, utilPath, "exports.util = 1\n")
	writeFile(t, root, ".gitignore", ".env.production\nnode_modules/\n")
	return root, deploycommittest.Repository(t, root, "https://github.com/acme/app.git")
}

func writeFile(t *testing.T, root, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o644))
}

// in names project paths under root, as the packagers report them.
func in(root string, paths ...string) []string {
	files := make([]string, len(paths))
	for i, path := range paths {
		files[i] = filepath.Join(root, filepath.FromSlash(path))
	}
	return files
}

func commitOf(t *testing.T, root string, files, reads []string) *api.DeployCommit {
	t.Helper()
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	require.NotNil(t, source)
	got := source.Commit(context.Background(), files, reads)
	require.NotNil(t, got)
	return got
}

func TestCommitIsCleanForAnUnchangedCheckout(t *testing.T) {
	root, head := newRepository(t)
	writeFile(t, root, "node_modules/left-pad/index.js", "module.exports = 1\n")

	// A deploy can run from any directory inside the repository.
	source := Detect(context.Background(), cliruntime.Deps{}, filepath.Join(root, "volcano"))
	got := source.Commit(context.Background(), in(root, helloPath, utilPath), in(root, "volcano/functions"))
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: head}, got)
}

func TestCommitIsDirtyWhenTheUploadDiffersFromTheCommit(t *testing.T) {
	cases := map[string]func(t *testing.T, root string) (files, reads []string){
		"an edited file": func(t *testing.T, root string) ([]string, []string) {
			t.Helper()
			writeFile(t, root, helloPath, "exports.handler = 2\n")
			return in(root, helloPath), nil
		},
		// git status lists neither of the next two, but both change what deploys.
		"an ignored file": func(t *testing.T, root string) ([]string, []string) {
			t.Helper()
			writeFile(t, root, ".env.production", "API_URL=https://example.com\n")
			return in(root, helloPath, ".env.production"), nil
		},
		"an assume-unchanged edit": func(t *testing.T, root string) ([]string, []string) {
			t.Helper()
			deploycommittest.Git(t, root, "update-index", "--assume-unchanged", helloPath)
			writeFile(t, root, helloPath, "exports.handler = 2\n")
			require.Empty(t, deploycommittest.Git(t, root, "status", "--porcelain"))
			return in(root, helloPath), nil
		},
		"an untracked file": func(t *testing.T, root string) ([]string, []string) {
			t.Helper()
			writeFile(t, root, "volcano/functions/new.js", "exports.handler = 3\n")
			return in(root, helloPath, "volcano/functions/new.js"), nil
		},
		// Each file's content is somewhere in the commit, but not at its path.
		"content moved between files": func(t *testing.T, root string) ([]string, []string) {
			t.Helper()
			writeFile(t, root, helloPath, "exports.util = 1\n")
			return in(root, helloPath, utilPath), nil
		},
		"a deleted file the upload would hold": func(t *testing.T, root string) ([]string, []string) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
			return in(root, helloPath), in(root, "volcano/functions")
		},
		"a deleted file the upload looked for": func(t *testing.T, root string) ([]string, []string) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
			return in(root, helloPath), in(root, utilPath)
		},
		"a changed executable bit": func(t *testing.T, root string) ([]string, []string) {
			t.Helper()
			if runtime.GOOS == "windows" {
				t.Skip("Windows has no executable bit")
			}
			require.NoError(t, os.Chmod(filepath.Join(root, helloPath), 0o755))
			return in(root, helloPath), nil
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			root, _ := newRepository(t)
			files, reads := change(t, root)
			assert.True(t, commitOf(t, root, files, reads).Dirty)
		})
	}
}

// Only what the upload reads counts: a change elsewhere in the repository
// leaves it clean.
func TestCommitIgnoresChangesOutsideTheUpload(t *testing.T) {
	root, _ := newRepository(t)
	require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
	writeFile(t, root, "README.md", "edited\n")

	assert.False(t, commitOf(t, root, in(root, helloPath), in(root, helloPath)).Dirty)
}

func TestCommitIgnoresTheExecutableBitWhereGitDoes(t *testing.T) {
	root, _ := newRepository(t)
	deploycommittest.Git(t, root, "config", "core.fileMode", "false")
	require.NoError(t, os.Chmod(filepath.Join(root, helloPath), 0o755))

	assert.False(t, commitOf(t, root, in(root, helloPath), nil).Dirty)
}

// A Windows checkout with core.autocrlf holds CRLF files that git stores as
// the commit's LF blobs, so it is clean.
func TestCommitComparesWhatGitWouldStore(t *testing.T) {
	root, _ := newRepository(t)
	deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
	var files []string
	// Enough files that git hash-object runs more than once.
	for i := range 300 {
		path := fmt.Sprintf("volcano/functions/%s/%03d.js", strings.Repeat("nested", 5), i)
		writeFile(t, root, path, "exports.n = 1\n")
		files = append(files, path)
	}
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "many")
	for _, path := range files {
		writeFile(t, root, path, "exports.n = 1\r\n")
	}

	assert.False(t, commitOf(t, root, in(root, files...), in(root, "volcano/functions")).Dirty)

	writeFile(t, root, files[299], "exports.n = 2\r\n")
	assert.True(t, commitOf(t, root, in(root, files...), nil).Dirty)
}

// A workflow can check out a ref other than the one that triggered it; the
// commit is whatever the deploy directory holds.
func TestCommitReportsTheCheckedOutCommit(t *testing.T) {
	root, first := newRepository(t)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "second")
	deploycommittest.Git(t, root, "checkout", "--quiet", "--detach", first)

	got := commitOf(t, root, in(root, helloPath), nil)
	assert.Equal(t, first, got.SHA)
	assert.False(t, got.Dirty)
}

func TestCommitIsDirtyForAFileOutsideTheRepository(t *testing.T) {
	root, _ := newRepository(t)
	outside := t.TempDir()
	writeFile(t, outside, "x.js", "x\n")

	assert.True(t, commitOf(t, root, []string{filepath.Join(outside, "x.js")}, nil).Dirty)
}

func TestCommitSendsNothingForAFileItCannotRead(t *testing.T) {
	root, _ := newRepository(t)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	require.NotNil(t, source)
	// Tracked, but gone after packaging read it.
	require.NoError(t, os.Remove(filepath.Join(root, helloPath)))

	assert.Nil(t, source.Commit(context.Background(), in(root, helloPath), nil))
}

func TestDetectWithRealGitOutsideARepository(t *testing.T) {
	_, _ = newRepository(t)
	assert.Nil(t, Detect(context.Background(), cliruntime.Deps{}, t.TempDir()))
}
