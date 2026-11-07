package deploycommit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/deploycommit/deploycommittest"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// app is where LavaFlow reads the repository newRepository makes.
var app = Target{Repository: "acme/app", Branch: "main"}

// pushed makes a repository as newRepository does, on branch main, whose
// origin/main holds HEAD, as after a push.
func pushed(t *testing.T) (root, head string) {
	t.Helper()
	root, head = newRepository(t)
	deploycommittest.Git(t, root, "branch", "-M", "main")
	deploycommittest.Git(t, root, "update-ref", "refs/remotes/origin/main", head)
	return root, head
}

// frontendUpload deploys the whole repository's web directory as a frontend
// does: the commit is read from it and compared with its files.
func frontendUpload(t *testing.T, root string) *Uploads {
	t.Helper()
	dir := filepath.Join(root, "web")
	source := Detect(context.Background(), cliruntime.Deps{}, dir)
	var files []File
	if source != nil {
		var paths []string
		for path := range source.tree {
			if within("web", path) {
				paths = append(paths, path)
			}
		}
		files = read(t, root, paths...)
	}
	uploads := &Uploads{}
	uploads.AddFrontend("web", dir, source, source.Commit(context.Background(), dir, files, nil))
	return uploads
}

func reasons(t *testing.T, uploads *Uploads, target Target) []Reason {
	t.Helper()
	got := uploads.Unreadable(context.Background(), cliruntime.Deps{}, target)
	if got == nil {
		return nil
	}
	return got.Reasons
}

func TestUnreadableIsNilForACleanPushedCommit(t *testing.T) {
	root, _ := pushed(t)
	uploads := frontendUpload(t, root)
	require.Equal(t, 1, uploads.Len())

	assert.Nil(t, uploads.Unreadable(context.Background(), cliruntime.Deps{}, app))
	// GitHub names are case-insensitive.
	assert.Nil(t, uploads.Unreadable(context.Background(), cliruntime.Deps{}, Target{Repository: "Acme/App", Branch: "main"}))
}

func TestUnreadableOutsideAGitCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv(OptOutEnv, "")
	dir := t.TempDir()
	uploads := &Uploads{}
	uploads.AddFrontend("web", dir, Detect(context.Background(), cliruntime.Deps{}, dir), nil)

	assert.Equal(t, &Unreadable{
		Resources: []string{"frontend web"},
		Uploads:   1,
		Reasons:   []Reason{{Kind: ReasonNotCheckout}},
	}, uploads.Unreadable(context.Background(), cliruntime.Deps{}, app))
}

func TestUnreadableWithoutAGitHubOrigin(t *testing.T) {
	root, _ := pushed(t)
	deploycommittest.Git(t, root, "remote", "set-url", "origin", "https://gitlab.com/acme/app.git")
	assert.Equal(t, []Reason{{Kind: ReasonNotGitHub}}, reasons(t, frontendUpload(t, root), app))

	deploycommittest.Git(t, root, "remote", "remove", "origin")
	assert.Equal(t, []Reason{{Kind: ReasonNotGitHub}}, reasons(t, frontendUpload(t, root), app))
}

func TestUnreadableBeforeTheFirstCommit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, helloPath, "exports.handler = 1\n")
	deploycommittest.Init(t, root, "https://github.com/acme/app.git")

	assert.Equal(t, []Reason{{Kind: ReasonNoCommit}}, reasons(t, frontendUpload(t, root), app))
}

func TestUnreadableWhenReportingIsOff(t *testing.T) {
	root, _ := pushed(t)
	t.Setenv(OptOutEnv, "off")

	assert.Equal(t, []Reason{{Kind: ReasonOptedOut}}, reasons(t, frontendUpload(t, root), app))
}

// failingGit fails every call with err.
type failingGit struct{ err error }

func (f failingGit) Run(context.Context, string, ...string) ([]byte, error) { return nil, f.err }

func TestUnreadableWhenGitIsMissingOrFails(t *testing.T) {
	t.Setenv(OptOutEnv, "")
	missing := cliruntime.Deps{GitCommandRunner: failingGit{err: &exec.Error{Name: "git", Err: exec.ErrNotFound}}}
	failing := cliruntime.Deps{GitCommandRunner: failingGit{err: errors.New("exit status 128")}}
	upload := func(dir string) *Uploads {
		uploads := &Uploads{}
		uploads.AddFunction("function", "hello", dir, nil, nil)
		return uploads
	}
	checkout := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(checkout, ".git"), 0o755))

	got := upload(t.TempDir()).Unreadable(context.Background(), missing, app)
	require.NotNil(t, got)
	assert.Equal(t, []Reason{{Kind: ReasonNoGit}}, got.Reasons)

	// A checkout git won't read, such as one another user owns.
	got = upload(checkout).Unreadable(context.Background(), failing, app)
	require.NotNil(t, got)
	assert.Equal(t, []Reason{{Kind: ReasonUnread}}, got.Reasons)
}

// The paths are those git status shows under the uploaded directory, in its
// order; a change elsewhere in the repository isn't named.
func TestUnreadableNamesUncommittedChanges(t *testing.T) {
	root, head := pushed(t)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	for _, path := range []string{"web/a.txt", "web/b.txt", "web/c.txt"} {
		writeFile(t, root, path, "new\n")
	}
	writeFile(t, root, ".gitignore", "changed\n")

	assert.Equal(t, []Reason{{
		Kind:  ReasonUncommitted,
		Paths: []string{helloPath, "web/a.txt", "web/b.txt", "web/c.txt"},
		SHA:   head,
	}}, reasons(t, frontendUpload(t, root), app))
}

// A function with no commit is one that changed, even where git status shows
// nothing; one dirty upload reports its directory's changes once.
func TestUnreadableNamesEachReasonOnce(t *testing.T) {
	root, head := pushed(t)
	source := Detect(context.Background(), cliruntime.Deps{}, root)
	require.NotNil(t, source)
	clean := &api.FunctionCommit{Repository: "acme/app", SHA: head}
	uploads := &Uploads{}
	uploads.AddFunction("function", "hello", root, source, nil)
	uploads.AddFunction("durable function", "orders", root, source, nil)
	uploads.AddFunction("function", "util", root, source, clean)

	assert.Equal(t, &Unreadable{
		Resources: []string{"function hello", "durable function orders"},
		Uploads:   3,
		Reasons:   []Reason{{Kind: ReasonDiffers, SHA: head}},
	}, uploads.Unreadable(context.Background(), cliruntime.Deps{}, app))

	writeFile(t, root, "volcano/functions/hello.js", "exports.handler = 1\n")
	got := uploads.Unreadable(context.Background(), cliruntime.Deps{}, app)
	require.NotNil(t, got)
	assert.Equal(t, []Reason{{Kind: ReasonUncommitted, Paths: []string{"volcano/"}, SHA: head}}, got.Reasons)
}

func TestUnreadableForAnotherRepository(t *testing.T) {
	root, head := pushed(t)

	assert.Equal(t, []Reason{{Kind: ReasonOtherRepository, SHA: head, Repository: "acme/app"}},
		reasons(t, frontendUpload(t, root), Target{Repository: "acme/web", Branch: "main"}))
}

func TestUnreadableForACommitNotOnTheProductionBranch(t *testing.T) {
	root, _ := pushed(t)
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "unpushed")
	head := deploycommittest.Git(t, root, "rev-parse", "HEAD")

	assert.Equal(t, []Reason{{Kind: ReasonOffBranch, SHA: head}}, reasons(t, frontendUpload(t, root), app))
	// No copy of the branch to ask: Volcano may still find the commit on it.
	assert.Nil(t, reasons(t, frontendUpload(t, root), Target{Repository: "acme/app", Branch: "release"}))
}

// A shallow clone may lack the history that joins HEAD to the branch.
func TestUnreadableTrustsAShallowCloneItCantCheck(t *testing.T) {
	origin, _ := pushed(t)
	// A clone of a single root commit has no history to cut off.
	writeFile(t, origin, utilPath, "exports.util = 2\n")
	deploycommittest.Git(t, origin, "commit", "--quiet", "-am", "second")
	root := filepath.Join(t.TempDir(), "clone")
	deploycommittest.Git(t, origin, "clone", "--quiet", "--depth", "1", "file://"+filepath.ToSlash(origin), root)
	deploycommittest.Git(t, root, "remote", "set-url", "origin", "https://github.com/acme/app.git")
	writeFile(t, root, helloPath, "exports.handler = 2\n")
	deploycommittest.Git(t, root, "commit", "--quiet", "-am", "unpushed")
	require.Equal(t, "true", deploycommittest.Git(t, root, "rev-parse", "--is-shallow-repository"))

	assert.Nil(t, reasons(t, frontendUpload(t, root), app))
}

func TestUnreadableIsNilOnceTheContextEnds(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv(OptOutEnv, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	uploads := &Uploads{}
	uploads.AddFrontend("web", t.TempDir(), nil, nil)

	assert.Nil(t, uploads.Unreadable(ctx, cliruntime.Deps{}, app))
}

func TestNilUploadsRecordNothing(t *testing.T) {
	var uploads *Uploads
	uploads.AddFrontend("web", t.TempDir(), nil, nil)
	uploads.AddFunction("function", "hello", t.TempDir(), nil, nil)

	assert.Equal(t, 0, uploads.Len())
	assert.Nil(t, uploads.Unreadable(context.Background(), cliruntime.Deps{}, app))
}
