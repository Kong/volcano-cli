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
	"github.com/Kong/volcano-cli/internal/ignore"
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

// upload records the frontend's two files and paths as read does: what an
// upload of the whole repository holds when nothing else is missing.
func upload(t *testing.T, root string, paths ...string) []File {
	t.Helper()
	return read(t, root, append([]string{helloPath, utilPath}, paths...)...)
}

// builtIn names what every upload leaves out, .gitignore among it, as the
// packager's built-in rules do.
func builtIn(t *testing.T) func(string) bool {
	t.Helper()
	matcher, err := ignore.NewProjectMatcher(t.TempDir())
	require.NoError(t, err)
	return matcher.MatchesPattern
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
	got := source.Commit(context.Background(), root, files, builtIn(t))
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
		source.Commit(context.Background(), filepath.Join(root, "web"), files, nil))
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: head, Root: ""},
		source.Commit(context.Background(), root, files, builtIn(t)))
}

func TestCommitIsDirtyWhenTheUploadDiffersFromTheCommit(t *testing.T) {
	cases := map[string]func(t *testing.T, root string) []File{
		"an edited file": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, helloPath, "exports.handler = 2\n")
			return upload(t, root)
		},
		// git status lists neither of the next two, but both change what deploys.
		"an ignored file": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, ".env.production", "API_URL=https://example.com\n")
			return upload(t, root, ".env.production")
		},
		"an assume-unchanged edit": func(t *testing.T, root string) []File {
			t.Helper()
			deploycommittest.Git(t, root, "update-index", "--assume-unchanged", helloPath)
			writeFile(t, root, helloPath, "exports.handler = 2\n")
			require.Empty(t, deploycommittest.Git(t, root, "status", "--porcelain"))
			return upload(t, root)
		},
		"an untracked file": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, "web/app/new.js", "exports.handler = 3\n")
			return upload(t, root, "web/app/new.js")
		},
		// Each file's content is somewhere in the commit, but not at its path.
		"content moved between files": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, helloPath, "exports.util = 1\n")
			return upload(t, root)
		},
		"a deleted file": func(t *testing.T, root string) []File {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
			return read(t, root, helloPath)
		},
		// A file the deploy leaves out, such as the env file volcano deploy
		// deploys as variables, is one the upload lacks.
		"a committed file the upload leaves out": func(t *testing.T, root string) []File {
			t.Helper()
			return read(t, root, helloPath)
		},
		// The packager skips what the root .gitignore lists, committed or not.
		"a committed file the root .gitignore lists": func(t *testing.T, root string) []File {
			t.Helper()
			writeFile(t, root, ".env.production", "API_URL=https://example.com\n")
			deploycommittest.Git(t, root, "add", "--force", ".env.production")
			deploycommittest.Git(t, root, "commit", "--quiet", "-m", "env")
			return upload(t, root)
		},
		// The archive holds regular files only, so it lacks the commit's file.
		"a file replaced by a symlink": func(t *testing.T, root string) []File {
			t.Helper()
			if runtime.GOOS == "windows" {
				t.Skip("making a symlink on Windows needs a privilege")
			}
			require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
			require.NoError(t, os.Symlink(filepath.Join("..", "app", "hello.js"), filepath.Join(root, utilPath)))
			return read(t, root, helloPath)
		},
		"a file replaced by a directory": func(t *testing.T, root string) []File {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
			require.NoError(t, os.Mkdir(filepath.Join(root, utilPath), 0o755))
			return read(t, root, helloPath)
		},
		// The packager follows no symlink, so it uploads nothing under one,
		// even where the files it points to are the commit's.
		"a directory replaced by a symlink": func(t *testing.T, root string) []File {
			t.Helper()
			if runtime.GOOS == "windows" {
				t.Skip("making a symlink on Windows needs a privilege")
			}
			elsewhere := t.TempDir()
			writeFile(t, elsewhere, "util.js", "exports.util = 1\n")
			require.NoError(t, os.RemoveAll(filepath.Join(root, "web", "lib")))
			require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "web", "lib")))
			return read(t, root, helloPath)
		},
		"an executable bit the archive keeps": func(t *testing.T, root string) []File {
			t.Helper()
			if runtime.GOOS == "windows" {
				t.Skip("Windows has no executable bit")
			}
			files := upload(t, root)
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

// A committed file every upload leaves out, such as an editor's settings,
// does not count as missing.
func TestCommitIsCleanWithoutWhatEveryUploadLeavesOut(t *testing.T) {
	root, _ := newRepository(t)
	writeFile(t, root, ".vscode/settings.json", "{}\n")
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "settings")

	assert.False(t, commitOf(t, root, upload(t, root)).Dirty)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	assert.True(t, source.Commit(context.Background(), root, upload(t, root), nil).Dirty, "nothing is left out")
}

// Only the uploaded directory counts: a change elsewhere in the repository
// leaves it clean.
func TestCommitIgnoresChangesOutsideTheUpload(t *testing.T) {
	root, _ := newRepository(t)
	files := read(t, root, helloPath)
	require.NoError(t, os.Remove(filepath.Join(root, utilPath)))
	require.NoError(t, os.Remove(filepath.Join(root, ".gitignore")))

	source := Detect(context.Background(), cliruntime.Deps{}, root)
	assert.False(t, source.Commit(context.Background(), filepath.Join(root, "web", "app"), files, nil).Dirty)
}

// The server compares each archive entry at its path under root, so a file
// from elsewhere in the repository cannot count as clean.
func TestCommitIsDirtyForAFileOutsideTheUploadedDirectory(t *testing.T) {
	root, _ := newRepository(t)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	assert.True(t, source.Commit(context.Background(), filepath.Join(root, "web", "app"), read(t, root, helloPath, utilPath), nil).Dirty)
}

func TestCommitSendsNothingForADirectoryOutsideTheRepository(t *testing.T) {
	root, _ := newRepository(t)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	assert.Nil(t, source.Commit(context.Background(), t.TempDir(), nil, nil))
}

// An archive that keeps no executable bit deploys the same either way, and
// where git does not track the bit there is nothing to compare.
func TestCommitComparesTheExecutableBitOnlyWhereBothKeepIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}
	root, _ := newRepository(t)
	require.NoError(t, os.Chmod(filepath.Join(root, helloPath), 0o755))
	assert.False(t, commitOf(t, root, upload(t, root)).Dirty, "the archive keeps no bit")

	files := upload(t, root)
	executable := true
	files[0].Executable = &executable
	deploycommittest.Git(t, root, "config", "core.fileMode", "false")
	assert.False(t, commitOf(t, root, files).Dirty, "git does not track the bit")
}

// The comparison uses the bytes packaging read, not the file as it is later.
func TestCommitComparesTheBytesPackagingRead(t *testing.T) {
	root, _ := newRepository(t)
	clean := upload(t, root)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	edited := upload(t, root)
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

	got := source.Commit(context.Background(), root, upload(t, root), builtIn(t))
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
		"an eol attribute": {func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, ".gitattributes", "*.js eol=lf\n")
		}, false},
		"a -text attribute despite core.autocrlf": {func(t *testing.T, root string) {
			t.Helper()
			deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
			writeFile(t, root, ".gitattributes", "*.js -text\n")
		}, true},
		"a legacy -crlf attribute despite core.autocrlf": {func(t *testing.T, root string) {
			t.Helper()
			deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
			writeFile(t, root, ".gitattributes", "*.js -crlf\n")
		}, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			root, _ := newRepository(t)
			tc.configure(t, root)
			writeFile(t, root, helloPath, "exports.handler = 1\r\n")
			assert.Equal(t, tc.dirty, commitOf(t, root, upload(t, root)).Dirty)
		})
	}
}

// text=auto, and core.autocrlf, leave a file git judges binary as it is, so
// only a text attribute makes its line endings count as the same.
func TestCommitComparesBinaryLineEndingsAsGitStoresThem(t *testing.T) {
	root, _ := newRepository(t)
	writeFile(t, root, "web/app/data.bin", "A\x00\n")
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "data")
	deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
	writeFile(t, root, "web/app/data.bin", "A\x00\r\n")

	assert.True(t, commitOf(t, root, upload(t, root, "web/app/data.bin")).Dirty)

	writeFile(t, root, ".gitattributes", "*.bin text\n")
	assert.False(t, commitOf(t, root, upload(t, root, "web/app/data.bin")).Dirty)
}

// Whether bytes are binary is git's own judgement: git hash-object stores
// them as they are when it judges them binary, and with LF when not. Each
// sample has a CRLF, so the two differ.
func TestBinaryMatchesGit(t *testing.T) {
	root, _ := newRepository(t)
	deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
	deploycommittest.Git(t, root, "config", "core.safecrlf", "false")
	for name, content := range map[string]string{
		"text":                             "a\r\nb\r\n",
		"a NUL":                            "A\x00\r\n",
		"a lone CR":                        "a\rb\r\n",
		"a final lone CR":                  "a\r\nb\r",
		"CR then CRLF":                     "\r\r\n",
		"control bytes":                    "\x01\x02\x03\r\n",
		"a final ^Z":                       "text\r\n\x1a",
		"a ^Z not at the end":              "\x1a\r\ntext",
		"1 control byte, 127 printable":    strings.Repeat("a", 127) + "\r\n\x01",
		"1 control byte, 128 printable":    strings.Repeat("a", 128) + "\r\n\x01",
		"2 control bytes, 253 printable":   strings.Repeat("a", 253) + "\r\n\x01\x02",
		"2 control bytes, 256 printable":   strings.Repeat("a", 256) + "\r\n\x01\x02",
		"2 DEL bytes, 128 printable":       strings.Repeat("a", 128) + "\r\n\x7f\x7f",
		"tabs, backspace, escape and feed": "a\tb\bc\033d\f\r\n",
		"UTF-8":                            "h\u00e9llo\r\n",
		"a NUL after 9000 bytes":           strings.Repeat("a", 9000) + "\r\n\x00",
	} {
		writeFile(t, root, "sample.txt", content)
		stored := deploycommittest.Git(t, root, "hash-object", "sample.txt")
		file := NewFile("sample.txt", []byte(content), nil)
		assert.Equal(t, stored == file.raw, file.binary, name)
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

	assert.False(t, commitOf(t, root, upload(t, root, paths...)).Dirty)

	writeFile(t, root, paths[299], "exports.n = 2\r\n")
	assert.True(t, commitOf(t, root, upload(t, root, paths...)).Dirty)
}

// A file Git LFS stores is committed as a pointer to its content. Without
// the filter attribute that names Git LFS, git checks the pointer out as it
// is, so content matching it is a change.
func TestCommitMatchesAGitLFSPointer(t *testing.T) {
	root, _ := newRepository(t)
	logo := []byte("\x89PNG not really\n")
	writeFile(t, root, ".gitattributes", "*.png filter=lfs diff=lfs merge=lfs -text\n")
	writeFile(t, root, "public/logo.png", string(lfsPointer(logo)))
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "logo")
	writeFile(t, root, "public/logo.png", string(logo))

	assert.False(t, commitOf(t, root, upload(t, root, ".gitattributes", "public/logo.png")).Dirty)

	writeFile(t, root, "public/logo.png", "\x89PNG another\n")
	assert.True(t, commitOf(t, root, upload(t, root, ".gitattributes", "public/logo.png")).Dirty)

	root, _ = newRepository(t)
	writeFile(t, root, "public/logo.png", string(lfsPointer(logo)))
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "logo")
	writeFile(t, root, "public/logo.png", string(logo))
	assert.True(t, commitOf(t, root, upload(t, root, "public/logo.png")).Dirty, "no filter attribute")
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

	assert.True(t, commitOf(t, root, upload(t, root)).Dirty)
	assert.NoFileExists(t, ran)
}

// The archive never holds a symlink, so a commit with one in the uploaded
// directory differs from the upload; one elsewhere does not count.
func TestCommitIsDirtyForACommittedSymlinkInTheUpload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("making a symlink on Windows needs a privilege")
	}
	root, _ := newRepository(t)
	require.NoError(t, os.Symlink("hello.js", filepath.Join(root, "web", "app", "alias.js")))
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "alias")
	source := Detect(context.Background(), cliruntime.Deps{}, root)

	assert.True(t, source.Commit(context.Background(), filepath.Join(root, "web"), read(t, root, helloPath, utilPath), nil).Dirty)
	assert.False(t, source.Commit(context.Background(), filepath.Join(root, "web", "lib"), read(t, root, utilPath), nil).Dirty)
}

// A replacement ref has git read another commit in place of the one HEAD
// names; the tree compared is still the reported commit's own.
func TestCommitComparesTheReportedCommitNotItsReplacement(t *testing.T) {
	root, first := newRepository(t)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "second")
	second := deploycommittest.Git(t, root, "rev-parse", "HEAD")
	deploycommittest.Git(t, root, "replace", first, second)
	deploycommittest.Git(t, root, "checkout", "--quiet", "--detach", first)

	got := commitOf(t, root, upload(t, root))
	assert.Equal(t, first, got.SHA)
	assert.True(t, got.Dirty)
}

// A workflow can check out a ref other than the one that triggered it; the
// commit is whatever the deploy directory holds.
func TestCommitReportsTheCheckedOutCommit(t *testing.T) {
	root, first := newRepository(t)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "second")
	deploycommittest.Git(t, root, "checkout", "--quiet", "--detach", first)

	got := commitOf(t, root, upload(t, root))
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
