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

const (
	headSHA  = "0123456789abcdef0123456789abcdef01234567"
	mergeSHA = "89abcdef0123456789abcdef0123456789abcdef"
	dir      = "/work/app"
)

// fakeGit answers the three git calls Detect makes. An empty answer for a
// call fails it, as git does outside a repository.
type fakeGit struct {
	remote, head, status string
	statusErr            error
	calls                []string
}

func (f *fakeGit) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	if name != "git" || len(args) < 3 || args[0] != "-C" || args[1] != dir {
		return nil, errors.New("unexpected command: " + name + " " + strings.Join(args, " "))
	}
	call := strings.Join(args[2:], " ")
	f.calls = append(f.calls, call)
	switch call {
	case "remote get-url origin":
		return answer(f.remote, nil)
	case "rev-parse HEAD":
		return answer(f.head, nil)
	case "status --porcelain":
		if f.statusErr != nil {
			return nil, f.statusErr
		}
		return []byte(f.status), nil
	}
	return nil, errors.New("unexpected git call: " + call)
}

func answer(value string, err error) ([]byte, error) {
	if value == "" {
		return nil, errors.New("exit status 128")
	}
	return []byte(value + "\n"), err
}

// clearEnv drops what the machine running the test sets, CI's own
// GITHUB_REPOSITORY above all.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"GITHUB_REPOSITORY", "GITHUB_SHA", "GITHUB_SERVER_URL", OptOutEnv} {
		t.Setenv(name, "")
	}
}

func detect(t *testing.T, git *fakeGit, deps cliruntime.Deps) *api.DeployCommit {
	t.Helper()
	deps.GitCommandRunner = git
	return Detect(context.Background(), deps, dir)
}

func cleanCheckout() *fakeGit {
	return &fakeGit{remote: "https://github.com/acme/app.git", head: headSHA}
}

func TestDetectReadsOriginAndHead(t *testing.T) {
	clearEnv(t)
	got := detect(t, cleanCheckout(), cliruntime.Deps{})
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: headSHA}, got)
}

func TestDetectReportsADirtyTree(t *testing.T) {
	clearEnv(t)
	git := cleanCheckout()
	git.status = " M volcano/functions/hello.js\n?? notes.txt\n"
	got := detect(t, git, cliruntime.Deps{})
	require.NotNil(t, got)
	assert.True(t, got.Dirty)
}

// A remote can carry a token; only owner/name ever leaves the machine.
func TestDetectSendsOwnerAndNameOnly(t *testing.T) {
	clearEnv(t)
	for _, remote := range []string{
		"https://x-access-token:ghs_secret@github.com/acme/app.git",
		"git@github.com:acme/app.git",
		"ssh://git@github.com/acme/app",
	} {
		git := cleanCheckout()
		git.remote = remote
		got := detect(t, git, cliruntime.Deps{})
		require.NotNil(t, got, remote)
		assert.Equal(t, "acme/app", got.Repository, remote)
	}
}

func TestDetectSendsNothingInLocalModeOrWhenTurnedOff(t *testing.T) {
	clearEnv(t)
	git := cleanCheckout()
	assert.Nil(t, detect(t, git, cliruntime.Deps{LocalMode: true}))

	for _, off := range []string{"off", "OFF", " Off "} {
		t.Setenv(OptOutEnv, off)
		assert.Nil(t, detect(t, git, cliruntime.Deps{}), off)
	}
	assert.Empty(t, git.calls, "git never runs when nothing is sent")
}

// The server refuses a malformed commit and deploys nothing, so anything it
// would refuse, or anything partial, is dropped rather than sent.
func TestDetectDropsWhatTheServerWouldRefuse(t *testing.T) {
	clearEnv(t)
	cases := map[string]*fakeGit{
		"no origin":         {head: headSHA},
		"not GitHub":        {remote: "https://gitlab.com/acme/app.git", head: headSHA},
		"no commit yet":     {remote: "https://github.com/acme/app.git"},
		"short SHA":         {remote: "https://github.com/acme/app.git", head: "0123456"},
		"SHA-256 repo":      {remote: "https://github.com/acme/app.git", head: strings.Repeat("ab", 32)},
		"status fails":      {remote: "https://github.com/acme/app.git", head: headSHA, statusErr: errors.New("exit status 128")},
		"invalid owner":     {remote: "https://github.com/ac_me/app.git", head: headSHA},
		"local path origin": {remote: "/srv/git/app.git", head: headSHA},
	}
	for name, git := range cases {
		assert.Nil(t, detect(t, git, cliruntime.Deps{}), name)
	}
}

func TestDetectSendsNothingWithoutGit(t *testing.T) {
	clearEnv(t)
	deps := cliruntime.Deps{GitCommandRunner: cliruntime.CommandRunnerFunc(
		func(context.Context, string, ...string) ([]byte, error) {
			return nil, errors.New(`exec: "git": executable file not found in $PATH`)
		})}
	assert.Nil(t, Detect(context.Background(), deps, dir))
}

func TestDetectPrefersGitHubActions(t *testing.T) {
	clearEnv(t)
	t.Setenv("GITHUB_REPOSITORY", "acme/app")
	t.Setenv("GITHUB_SHA", strings.ToUpper(headSHA))
	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	git := &fakeGit{remote: "https://github.com/someone/fork.git", head: mergeSHA, status: " M dist/app.js\n"}

	got := detect(t, git, cliruntime.Deps{})
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: headSHA, Dirty: true}, got)
	assert.Equal(t, []string{"status --porcelain"}, git.calls, "the remote and HEAD come from the workflow")
}

// On a pull_request event GITHUB_SHA, and the checkout's HEAD, is the merge
// commit GitHub made for the check. It is on no branch, so the server records
// it unverified; the CLI still reports it, with dirty from the checkout.
func TestDetectReportsAPullRequestMergeCommit(t *testing.T) {
	clearEnv(t)
	t.Setenv("GITHUB_EVENT_NAME", "pull_request")
	t.Setenv("GITHUB_REPOSITORY", "acme/app")
	t.Setenv("GITHUB_SHA", mergeSHA)
	git := &fakeGit{head: mergeSHA}

	got := detect(t, git, cliruntime.Deps{})
	assert.Equal(t, &api.DeployCommit{Repository: "acme/app", SHA: mergeSHA, Dirty: false}, got)
}

func TestDetectDropsAnIncompleteOrForeignWorkflow(t *testing.T) {
	cases := map[string]map[string]string{
		"repository only":          {"GITHUB_REPOSITORY": "acme/app"},
		"commit only":              {"GITHUB_SHA": headSHA},
		"GitHub Enterprise Server": {"GITHUB_REPOSITORY": "acme/app", "GITHUB_SHA": headSHA, "GITHUB_SERVER_URL": "https://ghe.example.com"},
		"not owner/name":           {"GITHUB_REPOSITORY": "acme/app/extra", "GITHUB_SHA": headSHA},
	}
	for name, env := range cases {
		clearEnv(t)
		for key, value := range env {
			t.Setenv(key, value)
		}
		// git would answer, but a workflow's own values are never mixed with it.
		assert.Nil(t, detect(t, cleanCheckout(), cliruntime.Deps{}), name)
	}
}
