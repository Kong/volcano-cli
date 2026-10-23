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

// read records files under root as a packager does, from their bytes now,
// in an archive that keeps no executable bit.
func read(t *testing.T, root string, paths ...string) []File {
	t.Helper()
	files := make([]File, len(paths))
	for i, path := range paths {
		full := filepath.Join(root, filepath.FromSlash(path))
		content, err := os.ReadFile(full)
		require.NoError(t, err)
		files[i] = NewFile(full, content, nil)
	}
	return files
}

// in names paths under root, as packagers report the paths they read.
func in(root string, paths ...string) []string {
	full := make([]string, len(paths))
	for i, path := range paths {
		full[i] = filepath.Join(root, filepath.FromSlash(path))
	}
	return full
}

func commitOf(t *testing.T, root string, files []File, reads []string) *api.DeployCommit {
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
	got := source.Commit(context.Background(), read(t, root, helloPath, utilPath), in(root, "volcano/functions"))
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: head}, got)
}

func TestCommitIsDirtyWhenTheUploadDiffersFromTheCommit(t *testing.T) {
	cases := map[string]func(t *testing.T, root string) (files []File, reads []string){
		"an edited file": func(t *testing.T, root string) ([]File, []string) {
			t.Helper()
			writeFile(t, root, helloPath, "exports.handler = 2\n")
			return read(t, root, helloPath), nil
		},
		// git status lists neither of the next two, but both change what deploys.
		"an ignored file": func(t *testing.T, root string) ([]File, []string) {
			t.Helper()
			writeFile(t, root, ".env.production", "API_URL=https://example.com\n")
			return read(t, root, helloPath, ".env.production"), nil
		},
		"an assume-unchanged edit": func(t *testing.T, root string) ([]File, []string) {
			t.Helper()
			deploycommittest.Git(t, root, "update-index", "--assume-unchanged", helloPath)
			writeFile(t, root, helloPath, "exports.handler = 2\n")
			require.Empty(t, deploycommittest.Git(t, root, "status", "--porcelain"))
			return read(t, root, helloPath), nil
		},
		"an untracked file": func(t *testing.T, root string) ([]File, []string) {
			t.Helper()
			writeFile(t, root, "volcano/functions/new.js", "exports.handler = 3\n")
			return read(t, root, helloPath, "volcano/functions/new.js"), nil
		},
		// Each file's content is somewhere in the commit, but not at its path.
		"content moved between files": func(t *testing.T, root string) ([]File, []string) {
			t.Helper()
			writeFile(t, root, helloPath, "exports.util = 1\n")
			return read(t, root, helloPath, utilPath), nil
		},
		"a deleted file the upload would hold": func(t *testing.T, root string) ([]File, []string) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
			return read(t, root, helloPath), in(root, "volcano/functions")
		},
		"a deleted file the upload looked for": func(t *testing.T, root string) ([]File, []string) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
			return read(t, root, helloPath), in(root, utilPath)
		},
		"an executable bit the archive keeps": func(t *testing.T, root string) ([]File, []string) {
			t.Helper()
			if runtime.GOOS == "windows" {
				t.Skip("Windows has no executable bit")
			}
			files := read(t, root, helloPath)
			executable := true
			files[0].Executable = &executable
			return files, nil
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
	files := read(t, root, helloPath)
	require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
	writeFile(t, root, "README.md", "edited\n")

	assert.False(t, commitOf(t, root, files, in(root, helloPath)).Dirty)
}

// An archive that keeps no executable bit deploys the same either way, and
// where git does not track the bit there is nothing to compare.
func TestCommitComparesTheExecutableBitOnlyWhereBothKeepIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}
	root, _ := newRepository(t)
	require.NoError(t, os.Chmod(filepath.Join(root, helloPath), 0o755))
	assert.False(t, commitOf(t, root, read(t, root, helloPath), nil).Dirty, "the archive keeps no bit")

	files := read(t, root, helloPath)
	executable := true
	files[0].Executable = &executable
	deploycommittest.Git(t, root, "config", "core.fileMode", "false")
	assert.False(t, commitOf(t, root, files, nil).Dirty, "git does not track the bit")
}

// The comparison uses the bytes packaging read, not the file as it is later.
func TestCommitComparesTheBytesPackagingRead(t *testing.T) {
	root, _ := newRepository(t)
	clean := read(t, root, helloPath)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	edited := read(t, root, helloPath)
	writeFile(t, root, helloPath, "exports.handler = 1\n")

	assert.False(t, commitOf(t, root, clean, nil).Dirty)
	assert.True(t, commitOf(t, root, edited, nil).Dirty)
}

// The tree is read at the commit Detect read, so the files of a checkout
// that moves on afterwards are not compared with it.
func TestDetectReadsTheTreeOfTheCommitItReports(t *testing.T) {
	root, first := newRepository(t)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "second")

	got := source.Commit(context.Background(), read(t, root, helloPath), nil)
	require.NotNil(t, got)
	assert.Equal(t, first, got.SHA)
	assert.True(t, got.Dirty)
}

// Line endings count as the same only where git converts them.
func TestCommitComparesLineEndingsAsGitStoresThem(t *testing.T) {
	cases := map[string]struct {
		configure func(t *testing.T, root string)
		dirty     bool
	}{
		"core.autocrlf": {func(t *testing.T, root string) {
			t.Helper()
			deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
		}, false},
		"a text attribute": {func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, ".gitattributes", "* text=auto\n")
		}, false},
		"no conversion": {func(*testing.T, string) {}, true},
		"a -text attribute despite core.autocrlf": {func(t *testing.T, root string) {
			t.Helper()
			deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
			writeFile(t, root, ".gitattributes", "*.js -text\n")
		}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root, _ := newRepository(t)
			tc.configure(t, root)
			writeFile(t, root, helloPath, "exports.handler = 1\r\n")
			assert.Equal(t, tc.dirty, commitOf(t, root, read(t, root, helloPath), nil).Dirty)
		})
	}
}

// Enough CRLF files that git check-attr runs more than once.
func TestCommitChecksManyFilesInBatches(t *testing.T) {
	root, _ := newRepository(t)
	var paths []string
	for i := range 300 {
		path := fmt.Sprintf("volcano/functions/%s/%03d.js", strings.Repeat("nested", 5), i)
		writeFile(t, root, path, "exports.n = 1\n")
		paths = append(paths, path)
	}
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "many")
	deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
	for _, path := range paths {
		writeFile(t, root, path, "exports.n = 1\r\n")
	}

	assert.False(t, commitOf(t, root, read(t, root, paths...), in(root, "volcano/functions")).Dirty)

	writeFile(t, root, paths[299], "exports.n = 2\r\n")
	assert.True(t, commitOf(t, root, read(t, root, paths...), nil).Dirty)
}

// A file Git LFS stores is committed as a pointer to its content.
func TestCommitMatchesAGitLFSPointer(t *testing.T) {
	root, _ := newRepository(t)
	logo := []byte("\x89PNG not really\n")
	writeFile(t, root, "public/logo.png", string(lfsPointer(logo)))
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "logo")
	writeFile(t, root, "public/logo.png", string(logo))

	assert.False(t, commitOf(t, root, read(t, root, "public/logo.png"), nil).Dirty)

	writeFile(t, root, "public/logo.png", "\x89PNG another\n")
	assert.True(t, commitOf(t, root, read(t, root, "public/logo.png"), nil).Dirty)
}

// Reading the commit and comparing an upload never runs a filter the
// repository configures; content a filter would change reads as changed.
func TestCommitRunsNoGitFilter(t *testing.T) {
	root, _ := newRepository(t)
	ran := filepath.Join(t.TempDir(), "ran")
	deploycommittest.Git(t, root, "config", "filter.probe.clean", "touch '"+ran+"'; cat")
	deploycommittest.Git(t, root, "config", "filter.probe.smudge", "touch '"+ran+"'; cat")
	writeFile(t, root, ".gitattributes", "*.js filter=probe\n")
	writeFile(t, root, helloPath, "exports.handler = 2\n")

	assert.True(t, commitOf(t, root, read(t, root, helloPath), in(root, "volcano/functions")).Dirty)
	assert.NoFileExists(t, ran)
}

// A workflow can check out a ref other than the one that triggered it; the
// commit is whatever the deploy directory holds.
func TestCommitReportsTheCheckedOutCommit(t *testing.T) {
	root, first := newRepository(t)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "second")
	deploycommittest.Git(t, root, "checkout", "--quiet", "--detach", first)

	got := commitOf(t, root, read(t, root, helloPath), nil)
	assert.Equal(t, first, got.SHA)
	assert.False(t, got.Dirty)
}

func TestCommitIsDirtyForAFileOutsideTheRepository(t *testing.T) {
	root, _ := newRepository(t)
	outside := t.TempDir()
	writeFile(t, outside, "x.js", "x\n")

	assert.True(t, commitOf(t, root, read(t, outside, "x.js"), nil).Dirty)
}

func TestDetectWithRealGitOutsideARepository(t *testing.T) {
	_, _ = newRepository(t)
	assert.Nil(t, Detect(context.Background(), cliruntime.Deps{}, t.TempDir()))
}
