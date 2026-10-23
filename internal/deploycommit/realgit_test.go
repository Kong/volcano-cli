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
	helloPath = "web/app/hello.js"
	utilPath  = "web/lib/util.js"
)

// newRepository makes a repository with a GitHub origin and one commit
// holding a frontend's two files and a .gitignore.
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

// commitOf reports the commit for an upload of the whole repository.
func commitOf(t *testing.T, root string, files []File) *api.DeployCommit {
	t.Helper()
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	require.NotNil(t, source)
	got := source.Commit(context.Background(), root, files)
	require.NotNil(t, got)
	return got
}

// root is the uploaded directory's path in the repository, "" for the
// repository itself, wherever in the repository the deploy runs.
func TestCommitIsCleanForAnUnchangedCheckout(t *testing.T) {
	root, head := newRepository(t)
	writeFile(t, root, "web/node_modules/left-pad/index.js", "module.exports = 1\n")
	source := Detect(context.Background(), cliruntime.Deps{}, filepath.Join(root, "web", "app"))
	files := read(t, root, helloPath, utilPath)

	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: head, Root: "web"},
		source.Commit(context.Background(), filepath.Join(root, "web"), files))
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: head, Root: ""},
		source.Commit(context.Background(), root, files))
}

func TestCommitIsDirtyWhenTheUploadDiffersFromTheCommit(t *testing.T) {
	cases := map[string]func(t *testing.T, root string) []File{
		"an edited file": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, helloPath, "exports.handler = 2\n")
			return read(t, root, helloPath)
		},
		// git status lists neither of the next two, but both change what deploys.
		"an ignored file": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, ".env.production", "API_URL=https://example.com\n")
			return read(t, root, helloPath, ".env.production")
		},
		"an assume-unchanged edit": func(t *testing.T, root string) []File {
			t.Helper()
			deploycommittest.Git(t, root, "update-index", "--assume-unchanged", helloPath)
			writeFile(t, root, helloPath, "exports.handler = 2\n")
			require.Empty(t, deploycommittest.Git(t, root, "status", "--porcelain"))
			return read(t, root, helloPath)
		},
		"an untracked file": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, "web/app/new.js", "exports.handler = 3\n")
			return read(t, root, helloPath, "web/app/new.js")
		},
		// Each file's content is somewhere in the commit, but not at its path.
		"content moved between files": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, helloPath, "exports.util = 1\n")
			return read(t, root, helloPath, utilPath)
		},
		"a deleted file": func(t *testing.T, root string) []File {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
			return read(t, root, helloPath)
		},
		"an executable bit the archive keeps": func(t *testing.T, root string) []File {
			t.Helper()
			if runtime.GOOS == "windows" {
				t.Skip("Windows has no executable bit")
			}
			files := read(t, root, helloPath)
			executable := true
			files[0].Executable = &executable
			return files
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			root, _ := newRepository(t)
			assert.True(t, commitOf(t, root, change(t, root)).Dirty)
		})
	}
}

// Only the uploaded directory counts: a change elsewhere in the repository
// leaves it clean.
func TestCommitIgnoresChangesOutsideTheUpload(t *testing.T) {
	root, _ := newRepository(t)
	files := read(t, root, helloPath)
	require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
	require.NoError(t, os.Remove(filepath.Join(root, ".gitignore")))

	source := Detect(context.Background(), cliruntime.Deps{}, root)
	assert.False(t, source.Commit(context.Background(), filepath.Join(root, "web", "app"), files).Dirty)
}

// The server compares each archive entry at its path under root, so a file
// from elsewhere in the repository cannot count as clean.
func TestCommitIsDirtyForAFileOutsideTheUploadedDirectory(t *testing.T) {
	root, _ := newRepository(t)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	assert.True(t, source.Commit(context.Background(), filepath.Join(root, "web", "app"), read(t, root, helloPath, utilPath)).Dirty)
}

func TestCommitSendsNothingForADirectoryOutsideTheRepository(t *testing.T) {
	root, _ := newRepository(t)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	assert.Nil(t, source.Commit(context.Background(), t.TempDir(), nil))
}

// An archive that keeps no executable bit deploys the same either way, and
// where git does not track the bit there is nothing to compare.
func TestCommitComparesTheExecutableBitOnlyWhereBothKeepIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}
	root, _ := newRepository(t)
	require.NoError(t, os.Chmod(filepath.Join(root, helloPath), 0o755))
	assert.False(t, commitOf(t, root, read(t, root, helloPath)).Dirty, "the archive keeps no bit")

	files := read(t, root, helloPath)
	executable := true
	files[0].Executable = &executable
	deploycommittest.Git(t, root, "config", "core.fileMode", "false")
	assert.False(t, commitOf(t, root, files).Dirty, "git does not track the bit")
}

// The comparison uses the bytes packaging read, not the file as it is later.
func TestCommitComparesTheBytesPackagingRead(t *testing.T) {
	root, _ := newRepository(t)
	clean := read(t, root, helloPath)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	edited := read(t, root, helloPath)
	writeFile(t, root, helloPath, "exports.handler = 1\n")

	assert.False(t, commitOf(t, root, clean).Dirty)
	assert.True(t, commitOf(t, root, edited).Dirty)
}

// The tree is read at the commit Detect read, so the files of a checkout
// that moves on afterwards are not compared with it.
func TestDetectReadsTheTreeOfTheCommitItReports(t *testing.T) {
	root, first := newRepository(t)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "second")

	got := source.Commit(context.Background(), root, read(t, root, helloPath))
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
			assert.Equal(t, tc.dirty, commitOf(t, root, read(t, root, helloPath)).Dirty)
		})
	}
}

// Enough CRLF files that git check-attr runs more than once.
func TestCommitChecksManyFilesInBatches(t *testing.T) {
	root, _ := newRepository(t)
	var paths []string
	for i := range 300 {
		path := fmt.Sprintf("web/%s/%03d.js", strings.Repeat("nested", 5), i)
		writeFile(t, root, path, "exports.n = 1\n")
		paths = append(paths, path)
	}
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "many")
	deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
	for _, path := range paths {
		writeFile(t, root, path, "exports.n = 1\r\n")
	}

	assert.False(t, commitOf(t, root, read(t, root, paths...)).Dirty)

	writeFile(t, root, paths[299], "exports.n = 2\r\n")
	assert.True(t, commitOf(t, root, read(t, root, paths...)).Dirty)
}

// A file Git LFS stores is committed as a pointer to its content.
func TestCommitMatchesAGitLFSPointer(t *testing.T) {
	root, _ := newRepository(t)
	logo := []byte("\x89PNG not really\n")
	writeFile(t, root, "public/logo.png", string(lfsPointer(logo)))
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "logo")
	writeFile(t, root, "public/logo.png", string(logo))

	assert.False(t, commitOf(t, root, read(t, root, "public/logo.png")).Dirty)

	writeFile(t, root, "public/logo.png", "\x89PNG another\n")
	assert.True(t, commitOf(t, root, read(t, root, "public/logo.png")).Dirty)
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

	assert.True(t, commitOf(t, root, read(t, root, helloPath)).Dirty)
	assert.NoFileExists(t, ran)
}

// A workflow can check out a ref other than the one that triggered it; the
// commit is whatever the deploy directory holds.
func TestCommitReportsTheCheckedOutCommit(t *testing.T) {
	root, first := newRepository(t)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "second")
	deploycommittest.Git(t, root, "checkout", "--quiet", "--detach", first)

	got := commitOf(t, root, read(t, root, helloPath))
	assert.Equal(t, first, got.SHA)
	assert.False(t, got.Dirty)
}

func TestCommitIsDirtyForAFileOutsideTheRepository(t *testing.T) {
	root, _ := newRepository(t)
	outside := t.TempDir()
	writeFile(t, outside, "x.js", "x\n")

	assert.True(t, commitOf(t, root, read(t, outside, "x.js")).Dirty)
}

func TestDetectWithRealGitOutsideARepository(t *testing.T) {
	_, _ = newRepository(t)
	assert.Nil(t, Detect(context.Background(), cliruntime.Deps{}, t.TempDir()))
}
