package git

import (
	"bytes"
	"net/http"
	"os"
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
	assert.Equal(t, []string{"git remote -v"}, runner.ran())
}

func TestConnectSendsTheRootDirectoryWhenGiven(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput},
		"", "connect", "--root-directory", "apps/api")
	require.NoError(t, err)
	assert.Equal(t, "apps/api", api.sentConnectBody()["root_directory"])
}

// Re-running connect reports no change, and still rewrites the binding. The
// read contract does not expose which GitHub connection the binding uses, so
// skipping the write is the only way to leave deploys pinned to a connection
// that no longer works while reporting one that does.
func TestConnectReportsNoChangeButStillRebinds(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "octo/storefront is already connected to this project.")
	assert.NotContains(t, out, "✓ Connected", "nothing the user can see changed")
	require.NotNil(t, api.sentConnectBody(), "the binding is refreshed so its connection cannot go stale")
	assert.Equal(t, gitConnectionID, api.sentConnectBody()["connection_id"])
}

// A revoked connection with another able to reach the same repository is the
// case the rebind exists for: the resolved connection reaches the binding.
func TestConnectRebindsThroughTheConnectionThatWorks(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connections = []map[string]any{
		revokedGitHubConnection(gitConnectionID, "stale"),
		githubConnection(otherConnection, "octo"),
	}
	api.installationsByConnection[otherConnection] = []map[string]any{
		installation(gitInstallation, "octo", "User", "all"),
	}

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Equal(t, otherConnection, api.sentConnectBody()["connection_id"],
		"the binding must move to the connection that can reach the repository")
	assert.Contains(t, out, "GitHub account: octo")
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

	assert.Contains(t, out, "Replacing the existing connection to acme/old-store.")
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
	// The rebind carries it forward rather than resetting it: an omitted flag
	// is not a request to clear anything.
	assert.Equal(t, "apps/api", api.sentConnectBody()["root_directory"])
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
	assert.Equal(t, "apps/api", api.sentConnectBody()["root_directory"])
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
	assert.NotContains(t, out, "Replacing the existing connection")
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
