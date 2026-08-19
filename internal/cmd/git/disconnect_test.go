package git

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDisconnectRemovesTheBindingWhenConfirmed(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "y\n", "disconnect")
	require.NoError(t, err)

	assert.Contains(t, out, "will stop deploying project Storefront ("+gitProjectID+")")
	assert.Contains(t, out, "Disconnect octo/storefront?")
	assert.Contains(t, out, "Disconnected octo/storefront")
	assert.Contains(t, out, "The repository itself was not changed.")
	assert.True(t, api.disconnectCalled())
}

func TestDisconnectStopsWhenDeclined(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "n\n", "disconnect")
	require.NoError(t, err)

	assert.Contains(t, out, "Cancelled.")
	assert.False(t, api.disconnectCalled())
}

func TestDisconnectWithYesSkipsThePrompt(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "disconnect", "--yes")
	require.NoError(t, err)

	assert.NotContains(t, out, "Disconnect octo/storefront?")
	assert.True(t, api.disconnectCalled())
}

// The binding is read before the delete so an unconnected project says so,
// rather than surfacing a 404 from a call that was never going to work.
func TestDisconnectReportsAnUnconnectedProject(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "disconnect", "--yes")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "not connected to a repository")
	assert.Contains(t, err.Error(), "volcano git connect")
	assert.False(t, api.disconnectCalled())
}

// Disconnect never reaches the git provider — it only reads and deletes the
// project's own binding — so a 503 there came from something in front of the
// API. Reporting it as a missing GitHub App would send the user somewhere with
// nothing to fix.
func TestDisconnectDoesNotBlameTheGitHubAppForAnUpstream503(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.projectStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "disconnect", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get the project's repository connection")
	assert.Contains(t, err.Error(), "503")
	assert.NotContains(t, err.Error(), "local mode")
	assert.False(t, api.disconnectCalled())
}

func TestDisconnectTakesNoArguments(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "disconnect", "octo/storefront")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}

// The delete names no repository, so it removes whatever is bound when it
// arrives. If the binding moved while the prompt was open, the user would be
// deleting something they were never shown.
func TestDisconnectRefusesWhenTheBindingMovedUnderIt(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedRepoID = gitRepositoryID
	api.connectedAfterRead = "acme/something-else"

	_, err := executeGitCommand(t, api.serve(), nil, "y\n", "disconnect")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "changed while this command was running")
	assert.Contains(t, err.Error(), "acme/something-else")
	assert.False(t, api.disconnectCalled(), "nothing the user did not see may be removed")
}
