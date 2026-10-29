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
func TestFunctionsTurnOffAndKeepTheOtherSettings(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.frontend = "web"

	out, err := executeGitCommand(t, api.serve(), nil, "", "functions", "off")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"auto_deploy_enabled": true,
		"deploy_functions":    false,
		"frontend_name":       "web",
	}, api.sentSettingsBody())
	assert.Contains(t, out, "Functions turned off")
	assert.Contains(t, out, "Nothing was removed. Functions already deployed stay up; pushes no longer redeploy them.")
	assert.Contains(t, out, "A push to main deploys: frontend web\n")
}

func TestFunctionsTurnOn(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.deployFunctions = false

	out, err := executeGitCommand(t, api.serve(), nil, "", "functions", "on")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"auto_deploy_enabled": true, "deploy_functions": true}, api.sentSettingsBody())
	assert.Contains(t, out, "Functions turned on")
	assert.Contains(t, out, "Nothing was deployed. The next push to main deploys the repository's functions.")
	assert.Contains(t, out, "A push to main deploys: functions\n")
}

// While auto-deploy is off no push deploys anything, so the next push is not
// promised to deploy the functions, and the step that would is named.
func TestFunctionsPromiseNothingWhileAutoDeployIsOff(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.autoDeploy = false
	api.deployFunctions = false

	out, err := executeGitCommand(t, api.serve(), nil, "", "functions", "on")
	require.NoError(t, err)

	assert.NotContains(t, out, "The next push to main deploys")
	assert.Contains(t, out, "Auto-deploy is off, so a push to main will not deploy anything.")
	assert.Contains(t, out, "Turn it on with volcano git auto-deploy on")
}

// Nothing selected at all deploys nothing on a push, so both ways to select
// something are named.
func TestFunctionsOffWithNoFrontendNamesBothWaysBack(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "functions", "off")
	require.NoError(t, err)

	assert.Contains(t, out, "neither functions nor a frontend is selected")
	assert.NotContains(t, out, "Deploy functions with", "the command that just ran is not suggested back")
	assert.Contains(t, out, "To deploy a frontend on push too, run volcano git frontend <name> --root <path>")
}

// CI and agents hold project access tokens, which this route accepts.
func TestFunctionsWorkWithAProjectAccessToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommand(t, api.serve(), nil, "", "functions", "off")
	require.NoError(t, err)
	assert.Equal(t, false, api.sentSettingsBody()["deploy_functions"])
}

func TestFunctionsRefuseAnythingButOnOrOff(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"functions"}, "say whether functions should be on or off, such as: on"},
		{[]string{"functions", "yes"}, `functions is either on or off, not "yes"`},
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

func TestFunctionsOnAProjectWithNothingConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "functions", "on")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Connect one with volcano git connect")
	assert.Nil(t, api.sentSettingsBody())
}

func TestFunctionsWhereNoRepositoryCanBeConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.providerStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "functions", "on")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git provider integration is not configured on this API")
	assert.Nil(t, api.sentSettingsBody())
}
