package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

func TestProductionBranchSetsOnlyTheProductionBranch(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"production_branch": "release"}, api.sentBranchBody())
	assert.Nil(t, api.sentConnectBody(), "the repository is not rebound")
	assert.Nil(t, api.sentRootBody())
	assert.Zero(t, api.accountRouteReads(), "nothing is resolved on GitHub")
	assert.Contains(t, out, "Production branch set to release")
	assert.Contains(t, out, "Repository: octo/storefront")
	assert.Contains(t, out, "Production branch: release")
	assert.Contains(t, out,
		"Nothing was redeployed. release stays the production branch if the repository's default branch changes.")
	assert.Contains(t, out, "A push to release deploys: functions")
}

// Following the default sends an empty branch, and the default comes back from
// the platform, which read it from GitHub: the CLI does not know it itself.
func TestProductionBranchResetFollowsTheDefaultBranch(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedBranch = "release"

	out, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "--reset")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"production_branch": ""}, api.sentBranchBody())
	assert.Contains(t, out, "Production branch now follows the repository's default branch (main).")
	assert.Contains(t, out, "Production branch: main")
	assert.NotContains(t, out, "stays the production branch")
	assert.Contains(t, out, "A push to main deploys: functions")
}

// A push deploys nothing while auto-deploy is off, so the command says that
// rather than implying the next push to the branch deploys.
func TestProductionBranchSaysWhenAPushDeploysNothing(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.autoDeploy = false

	out, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.NoError(t, err)
	assert.Contains(t, out, "Auto-deploy is off, so a push to release will not deploy anything.")
}

func TestProductionBranchSucceedsWhenTheDeploySettingsCannotBeRead(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.deploySettingsStatus = http.StatusInternalServerError

	out, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.NoError(t, err)
	assert.Contains(t, out, "Production branch set to release")
	assert.Contains(t, out, "its deploy settings could not be read")
}

// CI and agents hold project access tokens, which this route accepts.
func TestProductionBranchWorksWithAProjectAccessToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"production_branch": "release"}, api.sentBranchBody())
}

func TestProductionBranchRefusesWhatCannotNameABranch(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"production-branch"}, "name the branch a push must land on to deploy, such as main, or pass --reset"},
		{[]string{"production-branch", ""}, "the branch is empty; pass --reset to follow the repository's default branch"},
		{[]string{"production-branch", "   "}, "the branch is empty; pass --reset to follow the repository's default branch"},
		{[]string{"production-branch", "release", "--reset"}, "pass a branch or --reset, not both"},
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"

		_, err := executeGitCommand(t, api.serve(), nil, "", tc.args...)
		require.Error(t, err, "%q", tc.args)
		assert.Contains(t, err.Error(), tc.want, "%q", tc.args)
		assert.Nil(t, api.sentBranchBody(), "%q", tc.args)
	}
}

// The server checks a branch name and reads the default branch, and its reason
// is shown as it gave it.
func TestProductionBranchShowsTheServersRefusal(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		status  int
		message string
	}{
		{[]string{"release..x"}, http.StatusBadRequest, "That is not a valid Git branch name."},
		{
			[]string{"--reset"},
			http.StatusBadRequest,
			"The repository has no default branch. Name a production branch, or add an initial commit and try again.",
		},
		{
			[]string{"--reset"},
			http.StatusConflict,
			"The repository's default branch could not be read through the project owner's GitHub connection. " +
				"Reconnect GitHub, then try again.",
		},
		{[]string{"release"}, http.StatusForbidden, "project access token is read-only"},
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"
		api.branchStatus, api.branchMessage = tc.status, tc.message

		_, err := executeGitCommand(t, api.serve(), nil, "", append([]string{"production-branch"}, tc.args...)...)
		require.Error(t, err, "%q", tc.args)
		assert.Contains(t, err.Error(), "failed to set the production branch: ", "%q", tc.args)
		assert.Contains(t, err.Error(), tc.message, "%q", tc.args)
	}
}

func TestProductionBranchOnAProjectWithNothingConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this project is not connected to a repository")
	assert.Contains(t, err.Error(), "Connect one with volcano git connect")
}

// Where the API has no GitHub App, as in local mode, nothing can be connected,
// so that is the reason given rather than advice to connect.
func TestProductionBranchWhereNoRepositoryCanBeConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.providerStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "--reset")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git provider integration is not configured on this API")
	assert.Contains(t, err.Error(), "If this is local mode, that is expected")
	assert.NotContains(t, err.Error(), "Connect one with")
}

func TestProductionBranchReportsAMissingProject(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectMissing = true

	_, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the selected project does not exist")
	assert.Contains(t, err.Error(), "Select one with volcano use <project>")
}
