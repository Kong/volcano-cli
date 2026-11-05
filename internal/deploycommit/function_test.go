package deploycommit

import (
	"context"
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

// These tests run the installed git, like realgit_test.go.

const functionPath = "volcano/functions/hello.js"

// newFunctionRepository makes a repository with a GitHub origin and one commit
// holding a function, its source, and a .gitignore that ignores dist.
func newFunctionRepository(t *testing.T) (root, head string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, root, functionPath, "exports.handler = 1\n")
	writeFile(t, root, "src/hello.ts", "export const handler = 1\n")
	writeFile(t, root, ".gitignore", "/dist/\n")
	return root, deploycommittest.Repository(t, root, "https://github.com/acme/app.git")
}

// functionInputs are what packaging the function at path records: the file
// as read now, and that file alone as its input.
func functionInputs(t *testing.T, root, path string) Inputs {
	t.Helper()
	return Inputs{
		Dir:   root,
		Files: read(t, root, path),
		Feeds: func(p string) bool { return p == path },
	}
}

func functionCommit(t *testing.T, root string, in Inputs) *api.FunctionCommit {
	t.Helper()
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	require.NotNil(t, source)
	return source.FunctionCommit(context.Background(), in)
}

func TestFunctionCommitOnNoSourceOrInputsSendsNothing(t *testing.T) {
	var source *Source
	assert.Nil(t, source.FunctionCommit(context.Background(), Inputs{Feeds: func(string) bool { return true }}))

	root, head := newFunctionRepository(t)
	in := functionInputs(t, root, functionPath)
	assert.Equal(t, &api.FunctionCommit{Repository: "acme/app", SHA: head}, functionCommit(t, root, in))
	in.Feeds = nil
	assert.Nil(t, functionCommit(t, root, in), "inputs it cannot name")
}

// A file the upload reads, or an input it names, outside the repository
// leaves nothing to report.
func TestFunctionCommitSendsNothingForAnInputOutsideTheRepository(t *testing.T) {
	root, _ := newFunctionRepository(t)
	outside := t.TempDir()
	writeFile(t, outside, "x.js", "x\n")

	in := functionInputs(t, root, functionPath)
	in.Files = append(in.Files, read(t, outside, "x.js")...)
	assert.Nil(t, functionCommit(t, root, in))

	in = functionInputs(t, root, functionPath)
	in.Paths = []string{filepath.Join(outside, "volcano-config.yaml")}
	assert.Nil(t, functionCommit(t, root, in))
}

// Through a directory symlink, a deleted input would be looked for in the
// wrong place, so nothing is reported even while the files match.
func TestFunctionCommitSendsNothingThroughASymlinkedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("making a symlink on Windows needs a privilege")
	}
	root := t.TempDir()
	writeFile(t, root, "src/functions/hello.js", "exports.handler = 1\n")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "volcano"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join("..", "src", "functions"), filepath.Join(root, "volcano", "functions")))
	deploycommittest.Repository(t, root, "https://github.com/acme/app.git")

	assert.Nil(t, functionCommit(t, root, functionInputs(t, root, functionPath)))
}

// A submodule's files are another repository's, so a function with one among
// its inputs reports nothing, and so does build output in a repository with
// one.
func TestFunctionCommitSendsNothingForASubmodule(t *testing.T) {
	root, _ := newFunctionRepository(t)
	head := deploycommittest.Git(t, root, "rev-parse", "HEAD")
	deploycommittest.Git(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",volcano/functions/vendor")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "submodule")

	in := functionInputs(t, root, functionPath)
	assert.NotNil(t, functionCommit(t, root, in), "a submodule elsewhere")
	in.Feeds = func(p string) bool { return strings.HasPrefix(p, "volcano/functions/") }
	assert.Nil(t, functionCommit(t, root, in))

	writeFile(t, root, "dist/hello.js", "exports.handler = 1\n")
	assert.Nil(t, functionCommit(t, root, functionInputs(t, root, "dist/hello.js")))
}

// A file removed from the index but still on disk is a staged deletion.
func TestFunctionCommitSendsNothingForAnInputRemovedFromTheIndex(t *testing.T) {
	root, _ := newFunctionRepository(t)
	deploycommittest.Git(t, root, "rm", "--quiet", "--cached", functionPath)

	assert.Nil(t, functionCommit(t, root, functionInputs(t, root, functionPath)))
}

// Line endings count as the same where git converts them, as for frontends.
func TestFunctionCommitComparesLineEndingsAsGitStoresThem(t *testing.T) {
	root, head := newFunctionRepository(t)
	writeFile(t, root, functionPath, "exports.handler = 1\r\n")
	writeFile(t, root, "src/hello.ts", "export const handler = 1\r\n")
	writeFile(t, root, "dist/hello.js", "exports.handler = 1\n")
	clean := &api.FunctionCommit{Repository: "acme/app", SHA: head}

	assert.Nil(t, functionCommit(t, root, functionInputs(t, root, functionPath)), "git keeps CRLF as it is")
	assert.Nil(t, functionCommit(t, root, functionInputs(t, root, "dist/hello.js")), "git keeps CRLF as it is")

	deploycommittest.Git(t, root, "config", "core.autocrlf", "true")
	assert.Equal(t, clean, functionCommit(t, root, functionInputs(t, root, functionPath)))
	assert.Equal(t, clean, functionCommit(t, root, functionInputs(t, root, "dist/hello.js")))
}

// Reporting a function's commit runs no filter or file-system monitor the
// repository configures, even when it reads the whole repository.
func TestFunctionCommitRunsNoGitFilterOrMonitor(t *testing.T) {
	root, _ := newFunctionRepository(t)
	ran := filepath.Join(t.TempDir(), "ran")
	writeFile(t, root, ".gitattributes", "*.js filter=probe\n*.ts filter=probe\n")
	deploycommittest.Git(t, root, "config", "filter.probe.clean", "touch '"+ran+"'; cat")
	deploycommittest.Git(t, root, "config", "filter.probe.smudge", "touch '"+ran+"'; cat")
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "probe")
	deploycommittest.Git(t, root, "config", "core.fsmonitor", "touch '"+ran+"' #")
	head := deploycommittest.Git(t, root, "rev-parse", "HEAD")
	writeFile(t, root, "dist/hello.js", "exports.handler = 1\n")
	require.NoError(t, os.RemoveAll(ran))

	// git itself would run them.
	deploycommittest.Git(t, root, "status", "--porcelain")
	require.FileExists(t, ran)
	require.NoError(t, os.Remove(ran))

	assert.Equal(t, &api.FunctionCommit{Repository: "acme/app", SHA: head},
		functionCommit(t, root, functionInputs(t, root, "dist/hello.js")))
	assert.NoFileExists(t, ran)
}

// The whole repository matches the commit only with the executable bits it
// records, where git tracks them; a function's own upload keeps none.
func TestFunctionCommitComparesTheWholeRepositorysExecutableBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no executable bit")
	}
	root, head := newFunctionRepository(t)
	writeFile(t, root, "dist/hello.js", "exports.handler = 1\n")
	require.NoError(t, os.Chmod(filepath.Join(root, "src", "hello.ts"), 0o755))
	clean := &api.FunctionCommit{Repository: "acme/app", SHA: head}

	assert.Nil(t, functionCommit(t, root, functionInputs(t, root, "dist/hello.js")))
	assert.Equal(t, clean, functionCommit(t, root, functionInputs(t, root, functionPath)))

	deploycommittest.Git(t, root, "config", "core.fileMode", "false")
	assert.Equal(t, clean, functionCommit(t, root, functionInputs(t, root, "dist/hello.js")))
}

// A manifest reached through a symlink could stand in another committed
// file for the one the deploy names.
func TestFunctionCommitSendsNothingForASymlinkedPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("making a symlink on Windows needs a privilege")
	}
	root, _ := newFunctionRepository(t)
	require.NoError(t, os.Symlink("hello.js", filepath.Join(root, "volcano", "functions", "config.yaml")))
	deploycommittest.Git(t, root, "add", ".")
	deploycommittest.Git(t, root, "commit", "--quiet", "-m", "link")

	in := functionInputs(t, root, functionPath)
	in.Paths = []string{filepath.Join(root, "volcano", "functions", "config.yaml")}
	assert.Nil(t, functionCommit(t, root, in))
}

// A committed file another file or library takes the place of counts as
// uploaded only when the commit or the upload holds that replacement.
func TestFunctionCommitCountsAReplacementOnlyFromTheCommitOrTheUpload(t *testing.T) {
	root, head := newFunctionRepository(t)
	clean := &api.FunctionCommit{Repository: "acme/app", SHA: head}
	replacing := func(in Inputs, by string) Inputs {
		in.ShadowedBy = func(p string) string {
			if p == "src/hello.ts" {
				return by
			}
			return ""
		}
		return in
	}

	in := functionInputs(t, root, functionPath)
	assert.Equal(t, clean, functionCommit(t, root, replacing(in, functionPath)), "the commit holds it")
	assert.Equal(t, clean, functionCommit(t, root, replacing(in, "volcano")), "the commit holds files under it")
	assert.Nil(t, functionCommit(t, root, replacing(in, "volcano/functions/_x")), "nothing holds it")

	writeFile(t, root, "dist/hello.js", "exports.handler = 1\n")
	built := functionInputs(t, root, "dist/hello.js")
	assert.Equal(t, clean, functionCommit(t, root, replacing(built, "dist")), "the upload holds it")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "dist", "empty"), 0o755))
	assert.Nil(t, functionCommit(t, root, replacing(built, "dist/empty")), "nothing holds it")
}

// A file git neither tracks nor ignores at or under a root counts as a
// change, whether or not the upload holds it.
func TestFunctionCommitSendsNothingForAnUntrackedFileUnderARoot(t *testing.T) {
	root, head := newFunctionRepository(t)
	clean := &api.FunctionCommit{Repository: "acme/app", SHA: head}
	in := functionInputs(t, root, functionPath)
	in.Roots = []string{filepath.Join(root, "volcano", "functions")}
	writeFile(t, root, "dist/hello.js", "exports.handler = 1\n")
	writeFile(t, root, "src/notes.md", "notes\n")
	assert.Equal(t, clean, functionCommit(t, root, in), "untracked files elsewhere")

	writeFile(t, root, "volcano/functions/new/notes.md", "notes\n")
	assert.Nil(t, functionCommit(t, root, in))

	in.Roots = []string{filepath.Join(root, "volcano", "functions", "new", "notes.md")}
	assert.Nil(t, functionCommit(t, root, in), "a root in an untracked directory")
}
