package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

func TestRootDirectorySetsOnlyTheRootDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "root-directory", "apps/web")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"root_directory": "apps/web"}, api.sentRootBody())
	assert.Nil(t, api.sentConnectBody(), "the repository is not rebound")
	assert.Zero(t, api.accountRouteReads(), "nothing is resolved on GitHub")
	assert.Contains(t, out, "Root directory set to apps/web")
	assert.Contains(t, out, "Repository: octo/storefront")
	assert.Contains(t, out, "Nothing was redeployed. The next push to main builds from this directory.")
}

func TestRootDirectoryResetsToTheRepositoryRoot(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRoot = "apps/web"

	out, err := executeGitCommand(t, api.serve(), nil, "", "root-directory", "--reset")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"root_directory": ""}, api.sentRootBody())
	assert.Contains(t, out, "Root directory reset to the repository root")
	assert.NotContains(t, out, "Root directory: ")
}

// CI and agents hold project access tokens, which this route accepts.
func TestRootDirectoryWorksWithAProjectAccessToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommand(t, api.serve(), nil, "", "root-directory", "apps/web")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"root_directory": "apps/web"}, api.sentRootBody())
}

func TestRootDirectoryRefusesWhatCannotMeanADirectory(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"root-directory"}, "name the directory the project builds from, such as apps/web, or pass --reset"},
		{[]string{"root-directory", ""}, "the directory is empty; pass --reset to build from the repository root"},
		{[]string{"root-directory", "   "}, "the directory is empty; pass --reset to build from the repository root"},
		{[]string{"root-directory", "apps/web", "--reset"}, "pass a directory or --reset, not both"},
		{[]string{"root-directory", "/srv/app"}, "the root directory must be a path inside the repository, not an absolute path: /srv/app"},
		{[]string{"root-directory", "../outside"}, `the root directory must stay inside the repository, so it cannot contain "..": ../outside`},
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"

		_, err := executeGitCommand(t, api.serve(), nil, "", tc.args...)
		require.Error(t, err, "%q", tc.args)
		assert.Contains(t, err.Error(), tc.want, "%q", tc.args)
		assert.Nil(t, api.sentRootBody(), "%q", tc.args)
	}
}

// The server has the last word on a directory, and its reason is shown as it
// gave it.
func TestRootDirectoryShowsTheServersRefusal(t *testing.T) {
	for status, message := range map[int]string{
		http.StatusBadRequest: "root directory must be a relative path inside the repository",
		http.StatusConflict:   "the root directory can't be changed while the project's Git source is being handed over",
		http.StatusForbidden:  "project access token is read-only",
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"
		api.rootStatus, api.rootMessage = status, message

		_, err := executeGitCommand(t, api.serve(), nil, "", "root-directory", "apps/web")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set the root directory: ")
		assert.Contains(t, err.Error(), message)
	}
}

func TestRootDirectoryOnAProjectWithNothingConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "root-directory", "apps/web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this project is not connected to a repository")
	assert.Contains(t, err.Error(), "Connect one with volcano git connect")
}

// Where the API has no GitHub App, as in local mode, nothing can be connected,
// so that is the reason given rather than advice to connect.
func TestRootDirectoryWhereNoRepositoryCanBeConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.providerStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "root-directory", "apps/web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git provider integration is not configured on this API")
	assert.Contains(t, err.Error(), "If this is local mode, that is expected")
	assert.NotContains(t, err.Error(), "Connect one with")
}

// A project access token may not read the account's GitHub connections, so it
// cannot tell, and gets the plain answer.
func TestRootDirectoryWithAProjectTokenOnAProjectWithNothingConnected(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.providerStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "root-directory", "apps/web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this project is not connected to a repository")
	assert.Zero(t, api.accountRouteReads())
}

func TestRootDirectoryReportsAMissingProject(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectMissing = true

	_, err := executeGitCommand(t, api.serve(), nil, "", "root-directory", "apps/web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the selected project does not exist")
	assert.Contains(t, err.Error(), "Select one with volcano use <project>")
}
