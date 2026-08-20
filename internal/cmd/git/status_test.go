package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStatusReportsTheBinding(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRoot = "apps/api"
	api.frontend = "web"

	out, err := executeGitCommand(t, api.serve(), nil, "", "status")
	require.NoError(t, err)

	assert.Contains(t, out, "Project: Storefront ("+gitProjectID+")")
	assert.Contains(t, out, "Repository: octo/storefront")
	assert.Contains(t, out, "Production branch: main")
	assert.Contains(t, out, "Root directory: apps/api")
	assert.Contains(t, out, "A push to main deploys: functions, frontend web")
}

// Status reads the project's own binding and nothing else. Contacting GitHub
// would make a read of recorded state into a check of whether it still works,
// which is a different and much more expensive question.
func TestStatusContactsNeitherGitHubNorTheLocalRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	// Any provider call would fail outright, so a green run proves none happened.
	api.providerStatus = http.StatusServiceUnavailable
	runner := &gitRunner{stdout: originRemoteOutput}

	out, err := executeGitCommand(t, api.serve(), runner, "", "status")
	require.NoError(t, err)

	assert.Contains(t, out, "Repository: octo/storefront")
	assert.Empty(t, runner.ran(), "status has no reason to read the local repository")
	// Nothing is claimed about the GitHub account, because nothing was asked.
	assert.NotContains(t, out, "GitHub account")
}

// "Nothing is connected" is a complete answer to "what is connected?", so it
// exits 0 — otherwise the command is useless in a conditional.
func TestStatusReportsAnUnconnectedProjectWithoutFailing(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "", "status")
	require.NoError(t, err)

	assert.Contains(t, out, "Project: Storefront ("+gitProjectID+")")
	assert.Contains(t, out, "No repository is connected, so pushes do not deploy.")
	assert.Contains(t, out, "volcano git connect")
}

func TestStatusWarnsWhenTheSettingsCannotBeRead(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.deploySettingsStatus = http.StatusInternalServerError

	out, err := executeGitCommand(t, api.serve(), nil, "", "status")
	require.NoError(t, err)

	assert.Contains(t, out, "Repository: octo/storefront")
	assert.Contains(t, out, "deploy settings could not be read")
}

// A real failure reading the binding is still a failure.
func TestStatusFailsWhenTheBindingCannotBeRead(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectStatus = http.StatusInternalServerError

	_, err := executeGitCommand(t, api.serve(), nil, "", "status")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get the project's repository connection")
}

func TestStatusTakesNoArguments(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "status", "octo/storefront")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

// Status reports what is recorded; it does not announce a change.
func TestStatusDoesNotReadAsAnOutcome(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "status")
	require.NoError(t, err)

	assert.NotContains(t, out, "Connected")
	assert.NotContains(t, out, "already connected")
	assert.Nil(t, api.sentConnectBody(), "status must not write")
}

// The binding read answers 404 both for a project with no repository and for a
// project that does not exist. Reporting the benign reading for both would tell
// a script that an invalid selection is a valid unbound project.
func TestStatusDistinguishesAMissingProjectFromAnUnconnectedOne(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectMissing = true

	out, err := executeGitCommand(t, api.serve(), nil, "", "status")
	require.Error(t, err, "an invalid project selection must not exit 0")

	assert.Contains(t, err.Error(), "the selected project does not exist")
	assert.Contains(t, err.Error(), "VOLCANO_PROJECT_ID")
	assert.NotContains(t, out, "No repository is connected")
}

// A project that cannot be confirmed either way is not the same as one known
// not to exist, so the failure is reported as itself rather than resolved to the
// more definite reading. The binding read has to answer its own 404 here for the
// disambiguation to run at all.
func TestStatusDoesNotGuessWhenTheProjectCannotBeConfirmed(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectReadStatus = http.StatusInternalServerError

	_, err := executeGitCommand(t, api.serve(), nil, "", "status")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "failed to confirm the selected project exists")
	assert.NotContains(t, err.Error(), "does not exist")
	assert.NotContains(t, err.Error(), "No repository is connected")
}
