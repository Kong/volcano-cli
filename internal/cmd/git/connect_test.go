package git

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestConnectBindsTheRepositoryFromTheLocalRemote(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: originRemoteOutput}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Connected octo/storefront")
	assert.Contains(t, out, "Production branch: main")
	assert.Contains(t, out, "A push to main deploys: functions")

	body := api.sentConnectBody()
	require.NotNil(t, body)
	assert.Equal(t, gitConnectionID, body["connection_id"])
	assert.InDelta(t, float64(gitInstallation), body["installation_id"], 0)
	assert.InDelta(t, float64(gitRepositoryID), body["repository_id"], 0)
	// production_branch is deprecated: the branch follows GitHub's default, and
	// sending a stale one is rejected by the platform.
	assert.NotContains(t, body, "production_branch")
	assert.NotContains(t, body, "root_directory")
}

// The CLI must never write a credential into .git/config. The strongest local
// proof is that it only ever runs read commands against the repository.
func TestConnectOnlyReadsTheLocalRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: originRemoteOutput}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	// The exact set, so any new git invocation is a deliberate decision rather
	// than something that slipped in.
	assert.Equal(t, []string{
		"git remote -v",
		"git branch --show-current",
		"git config --get remote.pushDefault",
	}, runner.ran())
	for _, command := range runner.ran() {
		assert.True(t,
			strings.HasPrefix(command, "git remote -v") ||
				strings.HasPrefix(command, "git branch --show-current") ||
				strings.HasPrefix(command, "git config --get "),
			"every git invocation has to be one of the reads: %q", command)
	}
}

func TestConnectSendsTheRootDirectoryWhenGiven(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput},
		"", "connect", "--root-directory", "apps/api")
	require.NoError(t, err)
	assert.Equal(t, "apps/api", api.sentConnectBody()["root_directory"])
}

// A binding already recording everything the contract exposes has nothing to
// write, so nothing is written. The binding stores no connection id — deploys
// resolve the token from the project's owner — so a rewrite could not refresh
// one, and there is nothing else left to refresh.
func TestConnectSendsNothingWhenTheBindingIsAlreadyCorrect(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "octo/storefront is already connected to this project.")
	assert.NotContains(t, out, "✓ Connected", "nothing changed")
	assert.Nil(t, api.sentConnectBody(), "there is nothing left for a write to fix")
}

// The production branch is re-resolved from GitHub's live default by the bind,
// so a repository whose default moved has a stale one recorded — and that run
// is the one an operator needs told, not reported as unchanged.
func TestConnectRebindsWhenTheDefaultBranchMoved(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedBranch = "master"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "✓ Connected octo/storefront")
	assert.NotContains(t, out, "already connected")
	assert.NotNil(t, api.sentConnectBody())
	assert.Contains(t, out, "Production branch: main")
}

// GitHub preserves the case an owner typed but does not treat it as
// significant, so a differently-cased binding is the same binding.
func TestConnectTreatsRepositoryNamesCaseInsensitively(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "Octo/Storefront"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	// The full sentence, not just "already connected": the replacement prompt
	// contains that phrase too, so a substring match passes against a build
	// that fails to recognise the binding at all.
	assert.Contains(t, out, "is already connected to this project.")
	assert.NotContains(t, out, "Replace it with", "a differently-cased name is the same binding")
	assert.NotContains(t, out, "✓ Connected")
}

func TestConnectReplacesAnotherRepositoryOnlyOnConfirmation(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "n\n", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "is already connected to acme/old-store")
	assert.Contains(t, out, "Replace it with octo/storefront?")
	assert.Contains(t, out, "Cancelled.")
	assert.Nil(t, api.sentConnectBody(), "a declined replacement must not rebind")
}

func TestConnectReplacesAnotherRepositoryWhenConfirmed(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "y\n", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Replaced the existing connection to acme/old-store.")
	assert.Contains(t, out, "Connected octo/storefront")
	assert.NotNil(t, api.sentConnectBody())
}

func TestConnectWithYesSkipsTheReplacementPrompt(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
	assert.NotContains(t, out, "Replace it with")
}

// An explicitly named repository is confirmed, because the user may be pointing
// somewhere other than the checkout they are standing in.
func TestConnectConfirmsAnExplicitRepositoryURL(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "n\n",
		"connect", "https://github.com/octo/storefront.git")
	require.NoError(t, err)

	assert.Contains(t, out, "This will connect project Storefront (33333333-3333-4333-8333-333333333333) to octo/storefront.")
	assert.Contains(t, out, "Cancelled.")
	assert.Nil(t, api.sentConnectBody())
}

func TestConnectAcceptsAnExplicitRepositoryURLWithYes(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "",
		"connect", "git@github.com:octo/storefront.git", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
}

func TestConnectReportsNoGitHubConnection(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connections = nil

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "no GitHub account is connected")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
}

func TestConnectReportsARepositoryTheAppCannotSee(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.reposByInstallation[gitInstallation] = []map[string]any{repository("octo/something-else")}

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "not accessible through your GitHub connection")
	assert.Contains(t, err.Error(), "octo/storefront")
	assert.Contains(t, err.Error(), "selected repositories")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
}

// An installation scoped to selected repositories can carry a repo its account
// does not own, so a miss on the owner's installation must keep looking. The
// owner's installation is present here and does not hold the repo, so a lookup
// that stopped after the first installation would fail this.
func TestConnectFindsARepositoryThroughAnotherInstallation(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.installationsByConnection[gitConnectionID] = []map[string]any{
		installation(gitInstallation, "octo", "User", "selected"),
		installation(otherInstall, "acme", "Organization", "selected"),
	}
	api.reposByInstallation[gitInstallation] = []map[string]any{repository("octo/something-else")}
	api.reposByInstallation[otherInstall] = []map[string]any{repository("octo/storefront")}

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
	assert.InDelta(t, float64(otherInstall), api.sentConnectBody()["installation_id"], 0,
		"the binding must record the installation the repo was actually found through")
}

// A second connection must still be tried when the first cannot answer.
// Connection status is provider-defined free text, so an unusable connection
// is not reliably filtered out in advance — it shows up as a failing lookup.
func TestConnectKeepsLookingPastAFailingConnection(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connections = []map[string]any{
		githubConnection(gitConnectionID, "stale"),
		githubConnection(otherConnection, "octo"),
	}
	api.installationsStatus[gitConnectionID] = http.StatusInternalServerError
	api.installationsByConnection[otherConnection] = []map[string]any{
		installation(otherInstall, "octo", "User", "all"),
	}
	api.reposByInstallation[otherInstall] = []map[string]any{repository("octo/storefront")}

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
	assert.Equal(t, otherConnection, api.sentConnectBody()["connection_id"])
}

// Nothing resolved and something failed: the failure is what the user needs to
// see, not a generic "the App cannot see it".
func TestConnectReportsTheLookupFailureWhenNothingResolves(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.installationsStatus[gitConnectionID] = http.StatusInternalServerError

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list your GitHub App installations")
	assert.NotContains(t, err.Error(), "not accessible through your GitHub connection")
}

// GitHub preserves the case an owner typed but does not treat it as
// significant, so a differently-cased repository still resolves.
func TestConnectMatchesTheRepositoryCaseInsensitively(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.reposByInstallation[gitInstallation] = []map[string]any{repository("Octo/StoreFront")}

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected")
}

func TestConnectExplains503AsAMissingGitHubApp(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.providerStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "git provider integration is not configured")
	assert.Contains(t, err.Error(), "the local stack ships without GitHub App settings")
}

func TestConnectReportsAnEmptyRemoteList(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: ""}, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no remote URLs found in your Git config")
}

// An empty remote list is not on its own a refusal: a checkout with no remotes
// can still have a push route, since git follows a URL in the routing keys out
// of one. Reading the remote list first and failing on it made the URL route
// unreachable exactly where it is the only route there is.
func TestConnectFollowsAPushURLWithNoRemotesAtAll(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: "",
		outputs: map[string]string{
			"git config --get remote.pushDefault": "https://github.com/octo/storefront.git",
		},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Connected octo/storefront")
	assert.NotContains(t, out, "no remote URLs found")
	require.NotNil(t, api.sentConnectBody())
}

func TestConnectRejectsANonGitHubRemote(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: "origin\tgit@gitlab.com:octo/storefront.git (fetch)\n" +
		"origin\tgit@gitlab.com:octo/storefront.git (push)\n"}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `remote "origin"`)
	assert.Contains(t, err.Error(), "not a github.com repository")
}

// Auto-deploy off is the silent failure worth surfacing: the binding looks
// complete and a push still does nothing.
func TestConnectWarnsWhenAutoDeployIsOff(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.autoDeploy = false

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Connected octo/storefront")
	assert.Contains(t, out, "Auto-deploy is off")
}

// Replacing a binding and naming the repository explicitly are two reasons to
// confirm, but the user should only be asked once.
func TestConnectAsksOnceWhenReplacingAnExplicitRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"

	out, err := executeGitCommand(t, api.serve(), nil, "y\n",
		"connect", "https://github.com/octo/storefront.git")
	require.NoError(t, err)

	assert.Contains(t, out, "Replace it with octo/storefront?")
	assert.NotContains(t, out, "Connect it?")
	assert.Contains(t, out, "Connected octo/storefront")
}

// --root-directory is the only way to set or change the subdirectory a project
// builds from, so an already-connected repository must not short-circuit it.
func TestConnectUpdatesTheRootDirectoryOnAConnectedRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput},
		"", "connect", "--root-directory", "apps/api")
	require.NoError(t, err)

	require.NotNil(t, api.sentConnectBody(), "a root-directory change must be sent")
	assert.Equal(t, "apps/api", api.sentConnectBody()["root_directory"])
	assert.Contains(t, out, "Root directory: apps/api")
	// Nothing was replaced: it is the same repository.
	assert.NotContains(t, out, "Replacing the existing connection")
}

func TestConnectClearsTheRootDirectoryWhenExplicitlyEmptied(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRoot = "apps/api"

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput},
		"", "connect", "--root-directory", "")
	require.NoError(t, err)
	require.NotNil(t, api.sentConnectBody())
	assert.NotContains(t, api.sentConnectBody(), "root_directory")
}

func TestConnectLeavesAnUnmentionedRootDirectoryAlone(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRoot = "apps/api"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "already connected")
	assert.Contains(t, out, "Root directory: apps/api")
	assert.Nil(t, api.sentConnectBody(), "an omitted flag asks for no change, so nothing is written")
}

func TestConnectMatchingTheRootDirectoryIsStillIdempotent(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRoot = "apps/api"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput},
		"", "connect", "--root-directory", "apps/api")
	require.NoError(t, err)

	assert.Contains(t, out, "already connected", "asking for what is already set changes nothing")
	assert.Nil(t, api.sentConnectBody())
}

// Moving to a different repository starts from the root: the old
// subdirectory means nothing in the new one.
func TestConnectDoesNotCarryTheRootDirectoryToAnotherRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"
	api.connectedRoot = "apps/api"

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect", "--yes")
	require.NoError(t, err)
	require.NotNil(t, api.sentConnectBody())
	assert.NotContains(t, api.sentConnectBody(), "root_directory")
}

// A URL and --remote both say where the repository comes from, so the pair is
// refused rather than one of them winning silently.
func TestConnectRefusesARepositoryURLTogetherWithRemote(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: originRemoteOutput}

	_, err := executeGitCommand(t, api.serve(), runner, "",
		"connect", "https://github.com/octo/storefront.git", "--remote", "upstream", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--remote cannot be combined with a repository URL")
	assert.Nil(t, api.sentConnectBody())
}

// A repository address copied out of a browser carries a query string. Keeping
// it would send the user off to fix a permission problem that does not exist.
func TestConnectAcceptsARepositoryURLCopiedFromABrowser(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "",
		"connect", "https://github.com/octo/storefront?tab=readme-ov-file", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
}

func TestConnectReportsAFrontendAmongTheDeployTargets(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.frontend = "web"
	api.frontendRoot = "apps/web"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "A push to main deploys: functions, frontend web (apps/web)")
}

// Auto-deploy on with nothing selected is as silent as auto-deploy off.
func TestConnectWarnsWhenAutoDeployHasNothingToDeploy(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.deployFunctions = false

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "neither functions nor a frontend is selected")
}

// repo_full_name is cached at connect time and repo_id is the authoritative
// binding, so a repository renamed on GitHub is the same repository. Calling it
// a replacement would ask the user to confirm a destructive-sounding action
// that is not one.
func TestConnectTreatsARenamedRepositoryAsTheSameOne(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/old-name"
	api.connectedRepoID = gitRepositoryID

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.NotContains(t, out, "Replace it")
	assert.NotContains(t, out, "Replaced the existing connection")
	assert.Contains(t, out, "Connected octo/storefront", "the rebind refreshes the cached name")
	assert.NotNil(t, api.sentConnectBody())
}

// The same repository keeps its root directory across that rebind: nothing
// about it changed.
func TestConnectKeepsTheRootDirectoryAcrossARename(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/old-name"
	api.connectedRepoID = gitRepositoryID
	api.connectedRoot = "apps/api"

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Equal(t, "apps/api", api.sentConnectBody()["root_directory"])
}

// The new repository has no equivalent of the old subdirectory, so it resets.
// That decides what gets built, so it is said while the user can still decline.
func TestConnectWarnsThatReplacingResetsTheRootDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"
	api.connectedRoot = "apps/api"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "n\n", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, `The root directory "apps/api" will reset to the repository root.`)
}

// A prompt read from a closed stdin comes back as a decline, which would exit 0
// having done nothing — indistinguishable from success for the agents and CI
// jobs these commands serve.
func TestConnectRefusesToGuessWhenStdinCannotAnswer(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"

	closed, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closed.Close() })

	cmd := New(cliruntime.Deps{
		HTTPClient: api.serve().Client(), APIBaseURL: api.serve().URL,
		GitCommandRunner: &gitRunner{stdout: originRemoteOutput},
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(closed)
	cmd.SetArgs([]string{"connect"})

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin is not a terminal")
	assert.Contains(t, err.Error(), "--yes")
	assert.Nil(t, api.sentConnectBody())
}

// A stale local remote after a rename is likelier than a permission problem,
// and git keeps working through GitHub's redirect, so it goes unnoticed.
func TestConnectSuggestsAStaleRemoteWhenTheProjectIsAlreadyBound(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/current-name"
	api.reposByInstallation[gitInstallation] = []map[string]any{repository("octo/current-name")}

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the local remote may also simply be out of date")
	assert.Contains(t, err.Error(), "octo/current-name")
}

// One installation that cannot be listed must not hide a repository another
// one holds — the owner's is tried first and is the likeliest to be scoped away.
func TestConnectKeepsLookingPastAFailingInstallation(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.installationsByConnection[gitConnectionID] = []map[string]any{
		installation(gitInstallation, "octo", "User", "selected"),
		installation(otherInstall, "acme", "Organization", "all"),
	}
	api.repositoriesStatus[gitInstallation] = http.StatusInternalServerError
	api.reposByInstallation[otherInstall] = []map[string]any{repository("octo/storefront")}

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
}

func TestConnectRejectsAnEmptyRepositoryArgument(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: originRemoteOutput}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect", "   ", "--remote", "upstream")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repository URL is empty")
	assert.Empty(t, runner.ran(), "an empty argument must not fall back to remote discovery")
}

// With more than one connection failing, the first failure is the one to
// report: it is the one the user is most likely to have been using.
func TestConnectReportsTheFirstFailureNotTheLast(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connections = []map[string]any{
		githubConnection(gitConnectionID, "first"),
		githubConnection(otherConnection, "second"),
	}
	api.installationsStatus[gitConnectionID] = http.StatusInternalServerError
	api.installationsStatus[otherConnection] = http.StatusInternalServerError

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), gitConnectionID)
	assert.NotContains(t, err.Error(), otherConnection)
}

// GitHub frees a renamed repository's name for reuse, so a cached name can match
// a repository that is not the one bound. Only the id decides, and here it says
// this is a replacement — reporting "already connected" would name a binding
// that does not exist and leave pushes deploying nothing.
func TestConnectTreatsAReusedNameAsADifferentRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRepoID = 555555

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "y\n", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Replace it with octo/storefront?")
	assert.Contains(t, out, "Connected octo/storefront")
	assert.NotNil(t, api.sentConnectBody(), "the stale binding must be replaced")
}

// Uninstalling and reinstalling the App issues a new installation id, leaving
// the stored one pointing at nothing and no push deploying. Connect repairs it
// rather than reporting the binding as already correct.
func TestConnectRepairsABindingWithAStaleInstallation(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRepoID = gitRepositoryID
	api.connectedInstallation = 999999

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.NotContains(t, out, "already connected")
	require.NotNil(t, api.sentConnectBody())
	assert.InDelta(t, float64(gitInstallation), api.sentConnectBody()["installation_id"], 0)
}

// The platform builds from this path inside the repository, so a path that
// climbs out of it or starts at the filesystem root deploys nothing. Reporting
// success for a value that cannot work is worse than refusing it.
func TestConnectRejectsARootDirectoryOutsideTheRepository(t *testing.T) {
	setGitCommandTestHome(t)
	for name, root := range map[string]string{
		"absolute":  "/etc/passwd",
		"traversal": "../../escape",
		"climbing":  "apps/../../out",
	} {
		t.Run(name, func(t *testing.T) {
			api := newGitAPI(t)
			_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput},
				"", "connect", "--root-directory", root)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--root-directory")
			assert.Nil(t, api.sentConnectBody(), "nothing may be sent for a path that cannot build")
		})
	}
}

func TestConnectNamesTheProjectAndTheGitHubAccount(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Project: Storefront ("+gitProjectID+")")
	assert.Contains(t, out, "GitHub account: octo")
}

// More than one connection can reach the same repository, and the first usable
// one is taken. Which identity the binding depends on has to be reported.
func TestConnectNamesWhichGitHubAccountItBoundThrough(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connections = []map[string]any{
		githubConnection(gitConnectionID, "shared-bot"),
		githubConnection(otherConnection, "octo"),
	}

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "GitHub account: shared-bot")
}

// The project is what gets mutated, so the prompts name it too.
func TestConnectPromptsNameTheProject(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "n\n", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "Project Storefront ("+gitProjectID+") is already connected to acme/old-store")
}

// VOLCANO_PROJECT_ID overrides the selection without changing the project name
// stored beside it, so the stored name may belong to a different project.
// Printing it would name the wrong project with full confidence.
func TestConnectDoesNotNameTheProjectItCannotIdentify(t *testing.T) {
	setGitCommandTestHome(t)
	const otherProject = "44444444-4444-4444-8444-444444444444"
	t.Setenv("VOLCANO_PROJECT_ID", otherProject)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Project: "+otherProject)
	assert.NotContains(t, out, "Storefront", "the stored name belongs to a different project")
}

// A failed settings read must not fail the connect — the binding is already
// made — but it must not look like "no deploy is configured" either.
func TestConnectWarnsButSucceedsWhenTheSettingsCannotBeRead(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.deploySettingsStatus = http.StatusInternalServerError

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err, "the binding landed, so the command succeeds")

	assert.Contains(t, out, "Connected octo/storefront")
	assert.Contains(t, out, "deploy settings could not be read")
	assert.NotContains(t, out, "Auto-deploy is off", "unknown must not be reported as off")
	assert.NotNil(t, api.sentConnectBody())
}

func TestConnectWarnsOnAnUnchangedBindingWhenTheSettingsCannotBeRead(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.deploySettingsStatus = http.StatusServiceUnavailable

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "already connected")
	assert.Contains(t, out, "deploy settings could not be read")
}

// A remote with a separate pushurl names two repositories. A push is what
// deploys, so the push target is bound — and said out loud, because the remote
// the user thinks of as "origin" fetches from somewhere else.
func TestConnectBindsThePushTargetAndSaysSo(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: "" +
		"origin\thttps://github.com/acme/upstream.git (fetch)\n" +
		"origin\tgit@github.com:octo/storefront.git (push)\n"}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Connected octo/storefront")
	assert.Contains(t, out, "the push target is what deploys")
	assert.Contains(t, out, "acme/upstream.git")
}

// A query string can carry a credential of its own, and it is not part of what
// identifies the remote.
func TestConnectRedactsAQuerySecretFromTheError(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: "" +
		"origin\thttps://gitlab.com/octo/repo.git?private_token=CANARY (fetch)\n" +
		"origin\thttps://gitlab.com/octo/repo.git?private_token=CANARY (push)\n"}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "CANARY")
	assert.NotContains(t, err.Error(), "private_token")
}

// The bind is a full replace that names no prior state, and everything the
// command decided rests on a read taken before resolving and before the prompt.
// If the project moved in that window, the write would discard a binding the
// user was never shown.
func TestConnectRefusesWhenTheBindingMovedUnderIt(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"
	api.connectedAfterRead = "acme/somewhere-else"

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "y\n", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "changed while this command was running")
	assert.Contains(t, err.Error(), "acme/somewhere-else")
	assert.Contains(t, err.Error(), "volcano git status")
	assert.Nil(t, api.sentConnectBody(), "nothing the user did not see may be overwritten")
}

// A project that gained a binding while the command was running also changed,
// even though the command started from "nothing connected".
func TestConnectRefusesWhenSomethingElseConnectedFirst(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connectedAfterRead = "acme/raced-in"

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "changed while this command was running")
	assert.Contains(t, err.Error(), "acme/raced-in")
	assert.Nil(t, api.sentConnectBody())
}

// The mirror case: a project that lost its binding mid-command.
func TestConnectRefusesWhenTheBindingDisappeared(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"
	api.disconnectAfterRead = true

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "y\n", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "no repository connected")
	assert.Nil(t, api.sentConnectBody())
}

// A mutable part of the binding changing counts too: another actor editing the
// root directory leaves the repository alone, so the repository comparison would
// pass it. A write that went ahead would carry the value this command read and
// revert their edit.
func TestConnectRefusesWhenOnlyTheRootDirectoryMovedUnderIt(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRepoID = gitRepositoryID
	api.connectedRoot = "apps/old"
	api.rootAfterRead = "apps/new"

	// A root directory of its own is asked for, so there is a write to guard.
	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput},
		"", "connect", "--root-directory", "apps/mine")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "changed while this command was running")
	assert.Nil(t, api.sentConnectBody(), "apps/new must not be overwritten")
}

// Nothing is written when the binding already matches, so a concurrent edit
// cannot be reverted on that path at all — it simply is not touched.
func TestConnectWritesNothingSoCannotRevertAConcurrentEdit(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRepoID = gitRepositoryID
	api.connectedRoot = "apps/old"
	api.rootAfterRead = "apps/new"

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Nil(t, api.sentConnectBody())
}

func TestConnectReportsAMissingProject(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectMissing = true

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the selected project does not exist")
	assert.Nil(t, api.sentConnectBody())
}

// A push to a remote with several pushurl entries reaches all of them, so
// binding any one would leave the others deploying nowhere while the CLI
// claimed a single push target.
func TestConnectRefusesARemoteWithSeveralPushURLs(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: "" +
		"origin\thttps://github.com/octo/storefront.git (fetch)\n" +
		"origin\tgit@github.com:octo/mirror-one.git (push)\n" +
		"origin\tgit@github.com:octo/mirror-two.git (push)\n"}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "pushes to 2 repositories")
	assert.Contains(t, err.Error(), "pass the repository URL to choose")
	assert.Nil(t, api.sentConnectBody())
}

// Naming the repository explicitly is the way through, and it does not consult
// the remotes at all.
func TestConnectAcceptsAnExplicitURLDespiteSeveralPushURLs(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: "" +
		"origin\thttps://github.com/octo/storefront.git (fetch)\n" +
		"origin\tgit@github.com:octo/mirror-one.git (push)\n" +
		"origin\tgit@github.com:octo/mirror-two.git (push)\n"}

	out, err := executeGitCommand(t, api.serve(), runner, "",
		"connect", "git@github.com:octo/storefront.git", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
	assert.Empty(t, runner.ran(), "an explicit URL does not need the remotes")
}

// The help promised "the only remote, or origin" for a release in which the
// default was already where git pushes — so in a fork checkout the command's own
// help named a different repository from the one it bound. Anyone changing the
// default again has to come through here.
func TestConnectHelpDescribesThePushRoutingDefault(t *testing.T) {
	setGitCommandTestHome(t)
	out, err := executeGitCommand(t, newGitAPI(t).serve(), nil, "", "connect", "--help")
	require.NoError(t, err)

	assert.Contains(t, out, `wherever "git push" sends this branch`)
	// And the flag's own default, which repeated the stale claim on its own line.
	assert.Contains(t, out, "where git pushes this branch")
}

// git routes a bare push through remote.pushDefault, so in a fork checkout the
// repository that receives pushes — and so the one a deployment comes from —
// is routinely not origin. Binding origin regardless connects a repository the
// user never pushes to.
func TestConnectBindsWhereGitPushesNotOrigin(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: "" +
			"origin\tgit@github.com:acme/upstream.git (fetch)\n" +
			"origin\tgit@github.com:acme/upstream.git (push)\n" +
			"fork\tgit@github.com:octo/storefront.git (fetch)\n" +
			"fork\tgit@github.com:octo/storefront.git (push)\n",
		outputs: map[string]string{"git config --get remote.pushDefault": "fork"},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Connected octo/storefront")
	assert.Contains(t, out, `Using remote "fork": remote.pushDefault sends this branch's pushes there.`)
}

// Saying so matters only when it changed the answer.
func TestConnectStaysQuietWhenThePushRemoteIsOrigin(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout:  originRemoteOutput,
		outputs: map[string]string{"git config --get remote.pushDefault": "origin"},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)
	assert.NotContains(t, out, "Using remote")
}

// --remote is the user speaking, and outranks the configuration.
func TestConnectHonoursRemoteOverThePushRemote(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: "" +
			"origin\tgit@github.com:octo/storefront.git (fetch)\n" +
			"origin\tgit@github.com:octo/storefront.git (push)\n" +
			"fork\tgit@github.com:acme/elsewhere.git (fetch)\n" +
			"fork\tgit@github.com:acme/elsewhere.git (push)\n",
		outputs: map[string]string{"git config --get remote.pushDefault": "fork"},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect", "--remote", "origin")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
	assert.NotContains(t, out, "Using remote")
}

// Fetching over https and pushing over ssh names one repository, so there is
// nothing to report — the note used to fire on every such checkout.
func TestConnectStaysQuietWhenTransportsDifferButRepositoryDoesNot(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: "" +
		"origin\thttps://github.com/octo/storefront.git (fetch)\n" +
		"origin\tgit@github.com:octo/storefront.git (push)\n"}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Connected octo/storefront")
	assert.NotContains(t, out, "the push target is what deploys")
}

// A named remote git still lists must not be denied. Dropping it also moved the
// selection to another remote without saying so.
func TestConnectReportsARemoteThatHasNothingToPushTo(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: "" +
		"origin\t\n" +
		"upstream\tgit@github.com:acme/upstream.git (fetch)\n" +
		"upstream\tgit@github.com:acme/upstream.git (push)\n"}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect", "--remote", "origin")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `remote "origin" has nothing to push to`)
	assert.NotContains(t, err.Error(), "no remote named")
}

// An empty --root-directory resets to the repository root; whitespace is a
// mistyped value or an unset variable, and clearing on that would be silent.
func TestConnectRejectsAWhitespaceRootDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRoot = "apps/api"

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput},
		"", "connect", "--root-directory", "   ")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only whitespace")
	assert.Nil(t, api.sentConnectBody())
}

// An expired GitHub token is the likeliest way a working setup stops working,
// and it has to carry the same reconnect guidance as having no connection.
func TestConnectGuidesAReconnectOnAnExpiredConnection(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.providerStatus = http.StatusUnauthorized

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "needs reconnecting")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
}

// A push remote naming neither a remote nor a URL leaves nothing to connect, and
// falling back to origin would bind a repository this checkout does not push to.
func TestConnectRefusesAPushRemoteThatDoesNotExist(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout:  originRemoteOutput,
		outputs: map[string]string{"git config --get remote.pushDefault": "missing"},
	}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), `remote.pushDefault names "missing"`)
	assert.Contains(t, err.Error(), "neither a remote in this repository nor a repository URL")
	assert.Nil(t, api.sentConnectBody(), "origin must not be bound instead")
}

// git-push(1) takes "either a URL or the name of a remote", so a URL in the
// routing config is a working push route rather than a broken remote name — a
// bare `git push` really sends there. Binding origin instead would bind a
// repository this checkout does not push to.
func TestConnectFollowsAURLInThePushConfiguration(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: "" +
			"origin\tgit@github.com:acme/upstream.git (fetch)\n" +
			"origin\tgit@github.com:acme/upstream.git (push)\n",
		outputs: map[string]string{
			"git config --get remote.pushDefault": "https://github.com/octo/storefront.git",
		},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	// origin points at acme/upstream, which the App cannot see, so a fallback
	// to origin fails outright rather than binding the wrong repository quietly.
	assert.Contains(t, out, "Connected octo/storefront")
	// No remote in this repository describes it, so the URL is what gets named.
	assert.Contains(t, out,
		"Using remote.pushDefault: it sends this branch's pushes to https://github.com/octo/storefront.git.")
	body := api.sentConnectBody()
	require.NotNil(t, body)
	assert.InDelta(t, float64(gitRepositoryID), body["repository_id"], 0)
}

// The refusal for an unusable push route offers --remote as the way out, so it
// has to be a way out. Reading the routing before honouring --remote made the
// command refuse its own advice, and there was then no way to connect at all
// short of editing the Git config.
func TestConnectHonoursRemoteDespiteAnUnusablePushRoute(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: originRemoteOutput,
		// Set, and unusable: git fails on this rather than falling through.
		outputs: map[string]string{"git config --get remote.pushDefault": "   "},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect", "--remote", "origin")
	require.NoError(t, err)

	assert.Contains(t, out, "Connected octo/storefront")
	// Not merely tolerated — not read at all, since --remote outranks it.
	assert.NotContains(t, runner.ran(), "git config --get remote.pushDefault")
}

// And the route is still refused when nothing overrides it, so the guard above
// did not simply switch the check off.
func TestConnectStillRefusesAnUnusablePushRouteWithoutRemote(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout:  originRemoteOutput,
		outputs: map[string]string{"git config --get remote.pushDefault": "   "},
	}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote.pushDefault")
	assert.Nil(t, api.sentConnectBody())
}

// git rewrites a push destination before using it, so the repository bound has
// to come from the rewritten URL. Both URLs here are valid GitHub URLs, so
// binding the configured one would bind a repository the push never reaches
// without anything failing — the decoy is what the App cannot see.
func TestConnectBindsTheRewrittenPushURL(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: "",
		outputs: map[string]string{
			"git config --get remote.pushDefault": "https://github.com/octo/decoy.git",
			// A typo here makes the rewrite silently not apply, which fails this
			// test rather than passing it.
			`git config --get-regexp ^url\..*\.(push)?insteadof$`: "" +
				"url.https://github.com/octo/storefront.git.pushinsteadof https://github.com/octo/decoy.git\n",
		},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "Connected octo/storefront")
	assert.NotContains(t, out, "decoy")
	require.NotNil(t, api.sentConnectBody())
}

// The same route with a credential in it. CI rewrites leave a job token in these
// values, and the note prints on success — where nothing is going wrong to make
// anyone look twice at the output.
func TestConnectRedactsACredentialInThePushConfiguration(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: originRemoteOutput,
		outputs: map[string]string{
			"git config --get remote.pushDefault": "https://x-access-token:CANARYSECRET@github.com/octo/storefront.git",
		},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.NoError(t, err)

	assert.NotContains(t, out, "CANARYSECRET")
	assert.Contains(t, out, "***@github.com/octo/storefront.git")
	assert.Contains(t, out, "Connected octo/storefront")
}

// A push there succeeds; it just lands where Volcano cannot deploy from. The URL
// is still redacted, and the key is still named.
func TestConnectRefusesANonGitHubURLInThePushConfiguration(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: originRemoteOutput,
		outputs: map[string]string{
			"git config --get remote.pushDefault": "https://gitlab-ci-token:CANARYSECRET@gitlab.com/octo/storefront.git",
		},
	}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.Error(t, err)

	assert.NotContains(t, err.Error(), "CANARYSECRET")
	assert.Contains(t, err.Error(), "remote.pushDefault")
	assert.Contains(t, err.Error(), "not a github.com repository")
	assert.Nil(t, api.sentConnectBody(), "origin must not be bound instead")
}

// The key that set it is named, so the user knows what to fix.
func TestConnectNamesTheKeyBehindABrokenPushRemote(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout: originRemoteOutput,
		outputs: map[string]string{
			"git branch --show-current":               "main",
			"git config --get branch.main.pushRemote": "gone",
		},
	}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `branch.main.pushRemote names "gone"`)
}

// --remote is the user speaking and outranks a broken configuration, so it is
// still a way through.
func TestConnectHonoursRemoteDespiteABrokenPushRemote(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{
		stdout:  originRemoteOutput,
		outputs: map[string]string{"git config --get remote.pushDefault": "missing"},
	}

	out, err := executeGitCommand(t, api.serve(), runner, "", "connect", "--remote", "origin")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
}

// A real git failure reading the configuration is not "nothing is configured".
func TestConnectReportsAGitFailureReadingTheConfiguration(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := &gitRunner{stdout: originRemoteOutput, failConfig: true}

	_, err := executeGitCommand(t, api.serve(), runner, "", "connect")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not read this directory's Git repository")
	assert.Nil(t, api.sentConnectBody())
}
