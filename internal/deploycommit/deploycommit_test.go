package deploycommit

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const headSHA = "0123456789abcdef0123456789abcdef01234567"

// fakeGit answers the git calls Detect makes for a checkout in root whose
// HEAD holds no files. An empty answer for a call fails it, as git does
// outside a repository.
type fakeGit struct {
	remote, head, root string
	calls              []string
}

func (f *fakeGit) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "git" || len(args) < 3 || args[0] != "-C" {
		return nil, errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
	}
	call := strings.Join(args[2:], " ")
	f.calls = append(f.calls, call)
	switch call {
	case "remote get-url origin":
		return answer(f.remote)
	case "rev-parse HEAD":
		return answer(f.head)
	case "rev-parse --show-toplevel":
		return answer(f.root)
	case "ls-tree -r -z --full-tree HEAD":
		return nil, nil
	case "config --type=bool --get core.fileMode":
		return nil, errors.New("exit status 1")
	}
	return nil, errors.New("unexpected git call: " + call)
}

func answer(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("exit status 128")
	}
	return []byte(value + "\n"), nil
}

func checkout(t *testing.T) *fakeGit {
	t.Helper()
	return &fakeGit{remote: "https://github.com/acme/app.git", head: headSHA, root: t.TempDir()}
}

func detect(t *testing.T, git *fakeGit, deps cliruntime.Deps) *api.DeployCommit {
	t.Helper()
	t.Setenv(OptOutEnv, "")
	deps.GitCommandRunner = git
	return Detect(context.Background(), deps, git.root).Commit(context.Background(), nil, nil)
}

// A remote can carry a token; only owner/name ever leaves the machine.
func TestDetectSendsOwnerAndNameOnly(t *testing.T) {
	for _, remote := range []string{
		"https://x-access-token:ghs_secret@github.com/acme/app.git",
		"git@github.com:acme/app.git",
		"ssh://git@github.com/acme/app",
	} {
		git := checkout(t)
		git.remote = remote
		assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: headSHA}, detect(t, git, cliruntime.Deps{}), remote)
	}
}

// A workflow's variables name the commit that triggered it, which need not
// be what the deploy directory holds, so only the checkout is read.
func TestDetectIgnoresGitHubActionsVariables(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "someone/else")
	t.Setenv("GITHUB_SHA", strings.Repeat("f", 40))
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: headSHA}, detect(t, checkout(t), cliruntime.Deps{}))
}

func TestDetectSendsNothingInLocalModeOrWhenTurnedOff(t *testing.T) {
	git := checkout(t)
	assert.Nil(t, detect(t, git, cliruntime.Deps{LocalMode: true}))

	for _, off := range []string{"off", "OFF", " Off "} {
		t.Setenv(OptOutEnv, off)
		assert.Nil(t, Detect(context.Background(), cliruntime.Deps{GitCommandRunner: git}, git.root), off)
	}
	assert.Empty(t, git.calls, "git never runs when nothing is sent")
}

// The server refuses a malformed commit and deploys nothing, so anything it
// would refuse, or anything partial, is dropped rather than sent.
func TestDetectDropsWhatTheServerWouldRefuse(t *testing.T) {
	cases := map[string]func(*fakeGit){
		"no origin":         func(g *fakeGit) { g.remote = "" },
		"not GitHub":        func(g *fakeGit) { g.remote = "https://gitlab.com/acme/app.git" },
		"no commit yet":     func(g *fakeGit) { g.head = "" },
		"short SHA":         func(g *fakeGit) { g.head = "0123456" },
		"SHA-256 repo":      func(g *fakeGit) { g.head = strings.Repeat("ab", 32) },
		"no top level":      func(g *fakeGit) { g.root = "" },
		"invalid owner":     func(g *fakeGit) { g.remote = "https://github.com/ac_me/app.git" },
		"local path origin": func(g *fakeGit) { g.remote = "/srv/git/app.git" },
	}
	t.Setenv(OptOutEnv, "")
	for name, change := range cases {
		git := checkout(t)
		dir := git.root
		change(git)
		assert.Nil(t, Detect(context.Background(), cliruntime.Deps{GitCommandRunner: git}, dir), name)
	}
}

func TestDetectSendsNothingWithoutGit(t *testing.T) {
	t.Setenv(OptOutEnv, "")
	deps := cliruntime.Deps{GitCommandRunner: cliruntime.CommandRunnerFunc(
		func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New(`exec: "git": executable file not found in $PATH`)
		})}
	assert.Nil(t, Detect(context.Background(), deps, t.TempDir()))
}

func TestCommitOnNoSourceSendsNothing(t *testing.T) {
	var source *Source
	assert.Nil(t, source.Commit(context.Background(), []string{"index.js"}, nil))
}

// blobID must match git's own object IDs, or every deploy reads as dirty.
func TestBlobIDMatchesGit(t *testing.T) {
	for content, want := range map[string]string{
		"":        "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391",
		"hello\n": "ce013625030ba8dba906f756967f9e9ca394464a",
	} {
		got, err := blobID(strings.NewReader(content), int64(len(content)))
		require.NoError(t, err)
		assert.Equal(t, want, got, "%q", content)
	}
}

func TestParseTreeKeepsBlobsWithTheirModes(t *testing.T) {
	tree := "100644 blob aaaa\tsrc/a.js\x00100755 blob bbbb\tbin/run.sh\x00160000 commit cccc\tvendor/lib\x00"
	assert.Equal(t, map[string]treeEntry{
		"src/a.js":   {mode: "100644", blob: "aaaa"},
		"bin/run.sh": {mode: "100755", blob: "bbbb"},
	}, parseTree(tree))
}
