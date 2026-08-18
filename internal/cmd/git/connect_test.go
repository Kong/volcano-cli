package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestConnectIsIdempotentOnTheSameRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "octo/storefront is already connected to this project.")
	assert.Nil(t, api.sentConnectBody(), "an unchanged binding must not be rewritten")
}

// GitHub preserves the case an owner typed but does not treat it as
// significant, so a differently-cased binding is the same binding.
func TestConnectTreatsRepositoryNamesCaseInsensitively(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "Octo/Storefront"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "already connected")
	assert.Nil(t, api.sentConnectBody())
}

func TestConnectReplacesAnotherRepositoryOnlyOnConfirmation(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "acme/old-store"

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "n\n", "connect")
	require.NoError(t, err)

	assert.Contains(t, out, "already connected to acme/old-store")
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

	assert.Contains(t, out, "This will connect the current project to octo/storefront.")
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
	api.repositories = []map[string]any{repository("octo/something-else")}

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "not accessible through your GitHub connection")
	assert.Contains(t, err.Error(), "octo/storefront")
	assert.Contains(t, err.Error(), "selected repositories")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
}

// An installation scoped to selected repositories can carry a repo its account
// does not own, so a miss on the owner's installation keeps looking.
func TestConnectFindsARepositoryThroughAnotherInstallation(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.installations = []map[string]any{installation("acme", "Organization", "selected")}

	out, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.NoError(t, err)
	assert.Contains(t, out, "Connected octo/storefront")
}

func TestConnectExplains503AsAMissingGitHubApp(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.status = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), &gitRunner{stdout: originRemoteOutput}, "", "connect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "git provider integration is not configured")
	assert.Contains(t, err.Error(), "Local mode does not ship the GitHub App settings")
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
	runner := &gitRunner{stdout: "origin\tgit@gitlab.com:octo/storefront.git (fetch)\n"}

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
