package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

// Turning it on is confirmed first, then sends apply_migrations alongside the
// other settings exactly as they were read.
func TestMigrationsTurnOnAfterConfirmation(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.frontend = "web"

	out, err := executeGitCommand(t, api.serve(), nil, "yes\n", "migrations", "on")
	require.NoError(t, err)

	assert.Contains(t, out, "Anyone who can push to main can then run any SQL on them, so protect that branch first.")
	assert.Contains(t, out, "volcano databases migration baseline")
	assert.Contains(t, out, "Apply migrations on push?")
	assert.Equal(t, map[string]any{
		"auto_deploy_enabled": true,
		"deploy_functions":    true,
		"frontend_name":       "web",
		"apply_migrations":    true,
	}, api.sentSettingsBody())
	assert.Contains(t, out, "Migrations turned on")
	assert.Contains(t, out, "Nothing was applied. The next push to main applies its pending migrations before it deploys.")
	assert.Contains(t, out, "A push to main applies its migrations, then deploys: functions, frontend web\n")
}

func TestMigrationsOnDeclinedSavesNothing(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "no\n", "migrations", "on")
	require.NoError(t, err)
	assert.Contains(t, out, "Cancelled.")
	assert.Nil(t, api.sentSettingsBody())
}

// --yes is how CI and agents turn it on, with nobody to answer a prompt.
func TestMigrationsOnWithYesSkipsThePrompt(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "migrations", "on", "--yes")
	require.NoError(t, err)
	assert.NotContains(t, out, "Apply migrations on push?")
	assert.Equal(t, true, api.sentSettingsBody()["apply_migrations"])
}

// Turning it off needs no confirmation and undoes nothing already applied.
func TestMigrationsTurnOff(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.applyMigrations = true

	out, err := executeGitCommand(t, api.serve(), nil, "", "migrations", "off")
	require.NoError(t, err)

	assert.NotContains(t, out, "Apply migrations on push?")
	assert.Equal(t, map[string]any{
		"auto_deploy_enabled": true,
		"deploy_functions":    true,
		"apply_migrations":    false,
	}, api.sentSettingsBody())
	assert.Contains(t, out, "Migrations turned off")
	assert.Contains(t, out, "Nothing was undone. Applied migrations stay; pushes no longer apply new ones.")
	assert.Contains(t, out, "A push to main deploys: functions\n")
}

// The other setting commands leave apply_migrations out of the write, which the
// platform reads as "keep the saved value", so they never turn it off.
func TestOtherSettingsLeaveMigrationsAsSaved(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.applyMigrations = true

	out, err := executeGitCommand(t, api.serve(), nil, "", "functions", "off")
	require.NoError(t, err)
	assert.NotContains(t, api.sentSettingsBody(), "apply_migrations")
	assert.True(t, api.applyMigrations)
	assert.Contains(t, out, "so a push to main applies its migrations and deploys no code.")
}

// With nothing selected to deploy, a push still applies migrations on its own,
// so the summary must not say a push deploys nothing.
func TestMigrationsOnWithNothingElseToDeploy(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.deployFunctions = false

	out, err := executeGitCommand(t, api.serve(), nil, "", "migrations", "on", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Nothing was applied. The next push to main applies its pending migrations.\n")
	assert.Contains(t, out, "so a push to main applies its migrations and deploys no code.")
	assert.NotContains(t, out, "a push deploys nothing")
}

// The prompt names a repository and branch. If the binding moved while it was
// open, the user's yes would let a repository they never saw run SQL as the
// database owner.
func TestMigrationsOnRefusesWhenTheBindingMovedUnderIt(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRepoID = gitRepositoryID
	api.connectedAfterRead = "acme/something-else"

	out, err := executeGitCommand(t, api.serve(), nil, "yes\n", "migrations", "on")
	require.Error(t, err)
	assert.Contains(t, out, "octo/storefront")
	assert.Contains(t, err.Error(), "changed while this command was running")
	assert.Contains(t, err.Error(), "acme/something-else")
	assert.Nil(t, api.sentSettingsBody(), "nothing may be saved for a binding the user did not confirm")
	assert.False(t, api.applyMigrations)
}

func TestMigrationsPromiseNothingWhileAutoDeployIsOff(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.autoDeploy = false

	out, err := executeGitCommand(t, api.serve(), nil, "", "migrations", "on", "--yes")
	require.NoError(t, err)
	assert.NotContains(t, out, "The next push to main applies")
	assert.Contains(t, out, "Auto-deploy is off, so a push to main will not deploy anything.")
}

// Where migrations are unavailable the platform refuses turning them on; the
// CLI reports that and saves nothing else.
func TestMigrationsOnWhereTheyAreUnavailable(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.settingsWriteStatus = http.StatusBadRequest
	api.settingsWriteMessage = "Database migrations are not available."

	_, err := executeGitCommand(t, api.serve(), nil, "", "migrations", "on", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to turn migrations on")
	assert.False(t, api.applyMigrations)
}

func TestMigrationsRefuseAnythingButOnOrOff(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"migrations"}, "say whether migrations should be on or off, such as: on"},
		{[]string{"migrations", "true"}, `migrations is either on or off, not "true"`},
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

// Nothing connected is refused before the prompt, so nobody confirms a change
// that cannot be saved.
func TestMigrationsOnAProjectWithNothingConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "yes\n", "migrations", "on")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Connect one with volcano git connect")
	assert.NotContains(t, out, "Apply migrations on push?")
	assert.Nil(t, api.sentSettingsBody())
}
