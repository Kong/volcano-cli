package deploycommit

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const (
	headSHA = "0123456789abcdef0123456789abcdef01234567"
	dir     = "/work/app"
)

// committed is what HEAD holds in the fake checkout, by path.
var committed = map[string]string{
	"volcano/functions/hello.js": "exports.handler = 1\n",
	"package.json":               "{}\n",
}

// fakeGit answers the git calls Detect makes. An empty answer for a call
// fails it, as git does outside a repository.
type fakeGit struct {
	remote, head string
	files        map[string]string
	calls        []string
}

func (f *fakeGit) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "git" || len(args) < 3 || args[0] != "-C" || args[1] != dir {
		return nil, errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
	}
	call := strings.Join(args[2:], " ")
	f.calls = append(f.calls, call)
	switch call {
	case "remote get-url origin":
		return answer(f.remote)
	case "rev-parse HEAD":
		return answer(f.head)
	case "ls-tree -r -z --full-tree HEAD":
		if f.files == nil {
			return nil, errors.New("exit status 128")
		}
		return []byte(lsTree(f.files)), nil
	}
	return nil, errors.New("unexpected git call: " + call)
}

func answer(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("exit status 128")
	}
	return []byte(value + "\n"), nil
}

// lsTree writes what `git ls-tree -r -z` prints for these files.
func lsTree(files map[string]string) string {
	var out strings.Builder
	for _, path := range sortedKeys(files) {
		id, err := blobID(strings.NewReader(files[path]), int64(len(files[path])))
		if err != nil {
			panic(err)
		}
		fmt.Fprintf(&out, "100644 blob %s\t%s\x00", id, path)
	}
	return out.String()
}

// archive packs files into a gzip-compressed tar, as the packagers do.
func archive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range sortedKeys(files) {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(files[name])), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(files[name]))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func sortedKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cleanCheckout() *fakeGit {
	return &fakeGit{remote: "https://github.com/acme/app.git", head: headSHA, files: committed}
}

func detect(t *testing.T, git *fakeGit, deps cliruntime.Deps) *Source {
	t.Helper()
	t.Setenv(OptOutEnv, "")
	deps.GitCommandRunner = git
	return Detect(context.Background(), deps, dir)
}

// The packagers rename a single-file function to its runtime's entrypoint, so
// only content is compared, never paths.
func TestCommitIsCleanWhenEveryFileIsInHead(t *testing.T) {
	source := detect(t, cleanCheckout(), cliruntime.Deps{})
	got := source.Commit(archive(t, map[string]string{
		"index.js":     committed["volcano/functions/hello.js"],
		"package.json": committed["package.json"],
	}))
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: headSHA}, got)
}

func TestCommitIsDirtyWhenAFileDiffersFromHead(t *testing.T) {
	source := detect(t, cleanCheckout(), cliruntime.Deps{})
	cases := map[string]map[string]string{
		"edited":            {"index.js": "exports.handler = 2\n"},
		"not in the commit": {"index.js": committed["volcano/functions/hello.js"], ".env.production": "SECRET=1\n"},
		"empty and new":     {"index.js": committed["volcano/functions/hello.js"], "notes.txt": ""},
	}
	for name, files := range cases {
		got := source.Commit(archive(t, files))
		require.NotNil(t, got, name)
		assert.True(t, got.Dirty, name)
	}
}

func TestCommitSkipsEntriesTheCLIWrites(t *testing.T) {
	source := detect(t, cleanCheckout(), cliruntime.Deps{})
	files := map[string]string{"index.js": committed["volcano/functions/hello.js"], ".volcano-app-path": "web\n"}

	assert.True(t, source.Commit(archive(t, files)).Dirty)
	assert.False(t, source.Commit(archive(t, files), ".volcano-app-path").Dirty)
}

func TestCommitSendsNothingForAnUnreadableArchive(t *testing.T) {
	source := detect(t, cleanCheckout(), cliruntime.Deps{})
	assert.Nil(t, source.Commit([]byte("not a gzip stream")))
	assert.Nil(t, source.Commit(archive(t, map[string]string{"index.js": "x"})[:20]))
}

func TestCommitOnNoSourceSendsNothing(t *testing.T) {
	var source *Source
	assert.Nil(t, source.Commit(archive(t, map[string]string{"index.js": "x"})))
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

// A remote can carry a token; only owner/name ever leaves the machine.
func TestDetectSendsOwnerAndNameOnly(t *testing.T) {
	for _, remote := range []string{
		"https://x-access-token:ghs_secret@github.com/acme/app.git",
		"git@github.com:acme/app.git",
		"ssh://git@github.com/acme/app",
	} {
		git := cleanCheckout()
		git.remote = remote
		got := detect(t, git, cliruntime.Deps{}).Commit(archive(t, nil))
		require.NotNil(t, got, remote)
		assert.Equal(t, "acme/app", got.Repository, remote)
	}
}

// A workflow's variables name the commit that triggered it, which need not
// be what the deploy directory holds, so only the checkout is read.
func TestDetectIgnoresGitHubActionsVariables(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "someone/else")
	t.Setenv("GITHUB_SHA", strings.Repeat("f", 40))
	got := detect(t, cleanCheckout(), cliruntime.Deps{}).Commit(archive(t, nil))
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: headSHA}, got)
}

func TestDetectSendsNothingInLocalModeOrWhenTurnedOff(t *testing.T) {
	git := cleanCheckout()
	assert.Nil(t, detect(t, git, cliruntime.Deps{LocalMode: true}))

	for _, off := range []string{"off", "OFF", " Off "} {
		t.Setenv(OptOutEnv, off)
		git.calls = nil
		assert.Nil(t, Detect(context.Background(), cliruntime.Deps{GitCommandRunner: git}, dir), off)
		assert.Empty(t, git.calls, "git never runs when nothing is sent")
	}
}

// The server refuses a malformed commit and deploys nothing, so anything it
// would refuse, or anything partial, is dropped rather than sent.
func TestDetectDropsWhatTheServerWouldRefuse(t *testing.T) {
	origin := "https://github.com/acme/app.git"
	cases := map[string]*fakeGit{
		"no origin":         {head: headSHA, files: committed},
		"not GitHub":        {remote: "https://gitlab.com/acme/app.git", head: headSHA, files: committed},
		"no commit yet":     {remote: origin, files: committed},
		"short SHA":         {remote: origin, head: "0123456", files: committed},
		"SHA-256 repo":      {remote: origin, head: strings.Repeat("ab", 32), files: committed},
		"tree unreadable":   {remote: origin, head: headSHA},
		"invalid owner":     {remote: "https://github.com/ac_me/app.git", head: headSHA, files: committed},
		"local path origin": {remote: "/srv/git/app.git", head: headSHA, files: committed},
	}
	for name, git := range cases {
		assert.Nil(t, detect(t, git, cliruntime.Deps{}), name)
	}
}

func TestDetectSendsNothingWithoutGit(t *testing.T) {
	t.Setenv(OptOutEnv, "")
	deps := cliruntime.Deps{GitCommandRunner: cliruntime.CommandRunnerFunc(
		func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New(`exec: "git": executable file not found in $PATH`)
		})}
	assert.Nil(t, Detect(context.Background(), deps, dir))
}
