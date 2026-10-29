package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apierr "github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

func TestFrontendSelectsTheFrontendAPushDeploys(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web", "--root", "apps/web")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"auto_deploy_enabled": true,
		"deploy_functions":    true,
		"frontend_name":       "web",
		"frontend_app_root":   "apps/web",
	}, api.sentSettingsBody())
	assert.Nil(t, api.sentConnectBody(), "the repository is not rebound")
	assert.Zero(t, api.accountRouteReads(), "nothing is resolved on GitHub")
	assert.Contains(t, out, "Frontend set to web (apps/web)")
	assert.Contains(t, out, "Repository: octo/storefront")
	assert.Contains(t, out, "Nothing was redeployed. The next push to main deploys this frontend.")
	assert.Contains(t, out, "A push to main deploys: functions, frontend web (apps/web)\n")
	assert.NotContains(t, out, "To deploy a frontend")
}

// The write replaces every setting, so the ones this command does not change
// go back exactly as they were read, off included.
func TestFrontendKeepsTheOtherDeploySettings(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.autoDeploy = false
	api.deployFunctions = false

	out, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"auto_deploy_enabled": false,
		"deploy_functions":    false,
		"frontend_name":       "web",
	}, api.sentSettingsBody())
	assert.Contains(t, out, "Frontend set to web\n")
	assert.Contains(t, out, "Auto-deploy is off, so a push to main will not deploy anything.")
	assert.NotContains(t, out, "The next push to main deploys this frontend.",
		"no push deploys anything while auto-deploy is off")
}

func TestFrontendNoneDeploysNoFrontend(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.frontend = "web"
	api.frontendRoot = "apps/web"

	out, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "--none")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"auto_deploy_enabled": true,
		"deploy_functions":    true,
	}, api.sentSettingsBody())
	assert.Contains(t, out, "Frontend cleared, so a push deploys no frontend")
	assert.Contains(t, out, "A frontend already deployed stays up; pushes no longer redeploy it.")
	assert.Contains(t, out, "A push to main deploys: functions\n")
}

// CI and agents hold project access tokens, which this route accepts.
func TestFrontendWorksWithAProjectAccessToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web")
	require.NoError(t, err)
	assert.Equal(t, "web", api.sentSettingsBody()["frontend_name"])
}

func TestFrontendRefusesWhatCannotNameAFrontend(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"frontend"}, "name the frontend a push deploys, such as web, or pass --none to deploy no frontend"},
		{[]string{"frontend", "   "}, "the frontend name is empty; pass --none to deploy no frontend"},
		{[]string{"frontend", "web", "--none"}, "pass a frontend name or --none, not both"},
		{[]string{"frontend", "--none", "--root", "apps/web"}, "--root says where a frontend builds from, so it cannot be combined with --none"},
		{[]string{"frontend", "web", "--root", "/srv/web"}, "the frontend root must be a path inside the repository, not an absolute path: /srv/web"},
		{[]string{"frontend", "web", "--root", "../outside"}, `the frontend root must stay inside the repository, so it cannot contain "..": ../outside`},
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

// Saving the settings would make them the project's own, so connecting later
// would leave auto-deploy and functions off. A repository comes first.
func TestFrontendOnAProjectWithNothingConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this project is not connected to a repository")
	assert.Contains(t, err.Error(), "Connect one with volcano git connect")
	assert.Nil(t, api.sentSettingsBody())
}

// Where the API has no GitHub App, as in local mode, nothing can be connected,
// so that is the reason given rather than advice to connect.
func TestFrontendWhereNoRepositoryCanBeConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.providerStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git provider integration is not configured on this API")
	assert.Contains(t, err.Error(), "If this is local mode, that is expected")
	assert.NotContains(t, err.Error(), "Connect one with")
	assert.Nil(t, api.sentSettingsBody())
}

// The server has the last word, and its reason is shown as it gave it.
func TestFrontendShowsTheServersRefusal(t *testing.T) {
	for status, message := range map[int]string{
		http.StatusBadRequest: "frontend_app_root requires frontend_name",
		http.StatusConflict:   "this change would remove deploy coverage after Git has taken over",
		http.StatusForbidden:  "project access token is read-only",
	} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.connected = "octo/storefront"
		api.settingsWriteStatus, api.settingsWriteMessage = status, message

		_, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to set the frontend: ")
		assert.Contains(t, err.Error(), message)
	}
}

// The write replaces every setting, so without reading them first it would
// reset the others. Nothing is written when they cannot be read.
func TestFrontendWritesNothingWhenTheSettingsCannotBeRead(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.deploySettingsStatus = http.StatusInternalServerError

	_, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read the project's deploy settings")
	assert.Nil(t, api.sentSettingsBody())
}

func TestFrontendReportsAMissingProject(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectMissing = true

	_, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the selected project does not exist")
	assert.Nil(t, api.sentSettingsBody())
}

// A project deleted after its binding was read answers 404 on the settings.
// The API error stays the cause, so the exit code still says "not found".
func TestFrontendKeepsTheCauseWhenTheProjectGoesAway(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.deploySettingsStatus = http.StatusNotFound

	_, err := executeGitCommand(t, api.serve(), nil, "", "frontend", "web")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the selected project does not exist")
	assert.Equal(t, http.StatusNotFound, apierr.Status(err))
	assert.Nil(t, api.sentSettingsBody())
}
