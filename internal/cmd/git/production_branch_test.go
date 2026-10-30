package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

func TestProductionBranchPinsTheBranch(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"production_branch": "release"}, api.sentBranchBody())
	assert.Nil(t, api.sentConnectBody(), "the repository is not rebound")
	assert.Zero(t, api.accountRouteReads(), "nothing is resolved on GitHub")
	assert.Contains(t, out, "Production branch set to release")
	assert.Contains(t, out, "Production branch: release (pinned)")
	assert.Contains(t, out, "Pinned: renaming the default branch on GitHub no longer moves it. Nothing was deployed.")
	assert.Contains(t, out, "A push to release deploys: functions\n")
}

// An empty branch asks the platform to follow the default again, which only
// --follow-default sends.
func TestProductionBranchFollowsTheDefaultAgain(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedBranch, api.branchPinned = "release", true

	out, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "--follow-default")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"production_branch": ""}, api.sentBranchBody())
	assert.Contains(t, out, "Production branch follows the repository's default branch, main")
	assert.Contains(t, out, "Production branch: main (follows the default branch)")
	assert.Contains(t, out, "Renaming the default branch on GitHub moves the project with it.")
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
		{[]string{"production-branch"}, "name the branch a push must land on to deploy, such as release, or pass --follow-default"},
		{[]string{"production-branch", "  "}, "the branch is empty; pass --follow-default to follow the repository's default branch"},
		{[]string{"production-branch", "release", "--follow-default"}, "pass a branch or --follow-default, not both"},
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

// The server has the last word on a branch name and on reading the default,
// and its reason is shown as it gave it.
func TestProductionBranchShowsTheServersRefusal(t *testing.T) {
	for status, message := range map[int]string{
		http.StatusBadRequest:      "production_branch is not a valid Git branch name",
		http.StatusConflict:        "the project owner's GitHub connection needs reconnecting",
		http.StatusTooManyRequests: "GitHub rate limited reading the default branch",
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"
		api.branchStatus, api.branchMessage = status, message

		_, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set the production branch: ")
		assert.Contains(t, err.Error(), message)
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

	_, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git provider integration is not configured on this API")
	assert.NotContains(t, err.Error(), "Connect one with")
}

func TestProductionBranchReportsAMissingProject(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectMissing = true

	_, err := executeGitCommand(t, api.serve(), nil, "", "production-branch", "release")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the selected project does not exist")
}
