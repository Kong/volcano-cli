package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

// The write replaces every setting, so the ones this command does not change
// go back exactly as they were read, the frontend included.
func TestAutoDeployTurnsOffAndKeepsTheOtherSettings(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.frontend, api.frontendRoot = "web", "apps/web"

	out, err := executeGitCommand(t, api.serve(), nil, "", "auto-deploy", "off")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"auto_deploy_enabled": false,
		"deploy_functions":    true,
		"frontend_name":       "web",
		"frontend_app_root":   "apps/web",
	}, api.sentSettingsBody())
	assert.Nil(t, api.sentConnectBody(), "the repository is not rebound")
	assert.Contains(t, out, "Auto-deploy turned off")
	assert.Contains(t, out, "Nothing was removed. What is already deployed stays up; pushes to main no longer deploy.")
	assert.Contains(t, out, "Auto-deploy is off, so a push to main will not deploy anything.")
	assert.NotContains(t, out, "Turn it on with", "the command that just ran is not suggested back")
}

func TestAutoDeployTurnsOn(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.autoDeploy = false

	out, err := executeGitCommand(t, api.serve(), nil, "", "auto-deploy", "on")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"auto_deploy_enabled": true, "deploy_functions": true}, api.sentSettingsBody())
	assert.Contains(t, out, "Auto-deploy turned on")
	assert.Contains(t, out, "Nothing was deployed. Pushes to main deploy from now on.")
	assert.Contains(t, out, "A push to main deploys: functions\n")
	assert.Contains(t, out, "To deploy a frontend on push too, run volcano git frontend <name> --root <path>")
}

// With neither functions nor a frontend selected, turning auto-deploy on
// still deploys nothing on push, so the command promises no deploys and names
// the commands that select something.
func TestAutoDeployOnWithNothingSelectedPromisesNoDeploys(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.autoDeploy, api.deployFunctions = false, false

	out, err := executeGitCommand(t, api.serve(), nil, "", "auto-deploy", "on")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"auto_deploy_enabled": true, "deploy_functions": false}, api.sentSettingsBody())
	assert.Contains(t, out, "Auto-deploy turned on")
	assert.NotContains(t, out, "deploy from now on")
	assert.Contains(t, out, "Nothing was deployed.\n")
	assert.Contains(t, out, "Auto-deploy is on, but neither functions nor a frontend is selected, so a push deploys nothing.")
	assert.Contains(t, out, "Deploy functions with volcano git functions on")
	assert.Contains(t, out, "To deploy a frontend on push too, run volcano git frontend <name> --root <path>")
}

// CI and agents hold project access tokens, which this route accepts.
func TestAutoDeployWorksWithAProjectAccessToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommand(t, api.serve(), nil, "", "auto-deploy", "off")
	require.NoError(t, err)
	assert.Equal(t, false, api.sentSettingsBody()["auto_deploy_enabled"])
}

func TestAutoDeployRefusesAnythingButOnOrOff(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"auto-deploy"}, "say whether auto-deploy should be on or off, such as: on"},
		{[]string{"auto-deploy", "on", "off"}, "say whether auto-deploy should be on or off, such as: on"},
		{[]string{"auto-deploy", "true"}, `auto-deploy is either on or off, not "true"`},
		{[]string{"auto-deploy", "ON"}, `auto-deploy is either on or off, not "ON"`},
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"

		_, err := executeGitCommand(t, api.serve(), nil, "", tc.args...)
		require.Error(t, err, "%q", tc.args)
		assert.Contains(t, err.Error(), tc.want, "%q", tc.args)
		assert.Nil(t, api.sentSettingsBody(), "%q", tc.args)
	}
}

func TestAutoDeployOnAProjectWithNothingConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "auto-deploy", "on")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this project is not connected to a repository")
	assert.Contains(t, err.Error(), "Connect one with volcano git connect")
	assert.Nil(t, api.sentSettingsBody())
}

// Where the API has no GitHub App, as in local mode, nothing can be connected,
// so that is the reason given rather than advice to connect.
func TestAutoDeployWhereNoRepositoryCanBeConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.providerStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "auto-deploy", "on")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git provider integration is not configured on this API")
	assert.NotContains(t, err.Error(), "Connect one with")
	assert.Nil(t, api.sentSettingsBody())
}

// The server has the last word, and its reason is shown as it gave it.
func TestAutoDeployShowsTheServersRefusal(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.settingsWriteStatus = http.StatusConflict
	api.settingsWriteMessage = "this change would remove deploy coverage after Git has taken over"

	_, err := executeGitCommand(t, api.serve(), nil, "", "auto-deploy", "off")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to turn auto-deploy off: ")
	assert.Contains(t, err.Error(), api.settingsWriteMessage)
}
