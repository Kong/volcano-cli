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

func TestDisconnectExplains503AsAMissingGitHubApp(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.status = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "disconnect", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Local mode does not ship the GitHub App settings")
}

func TestDisconnectTakesNoArguments(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "disconnect", "octo/storefront")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown command")
}
