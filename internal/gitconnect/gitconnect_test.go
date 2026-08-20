package gitconnect

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
)

// Only the provider is filtered on. Status is never anything but "active" —
// disconnecting deletes the row — so filtering on it would drop nothing, and a
// dead connection shows up as a 401 rather than in this list.
func TestGitHubConnectionsFiltersOnProviderAlone(t *testing.T) {
	t.Parallel()
	connections := []apiclient.GitConnection{
		{Provider: "github", Status: "active", ProviderLogin: "octo"},
		{Provider: "gitlab", Status: "active", ProviderLogin: "elsewhere"},
		{Provider: "GitHub", Status: "", ProviderLogin: "uppercase"},
		{Provider: "github", Status: "revoked", ProviderLogin: "kept-anyway"},
	}

	usable := githubConnections(connections)
	require.Len(t, usable, 3)
	assert.Equal(t, "octo", usable[0].ProviderLogin)
	assert.Equal(t, "uppercase", usable[1].ProviderLogin)
	assert.Equal(t, "kept-anyway", usable[2].ProviderLogin)
}

// An expired GitHub token is the likeliest way a working setup stops working,
// and its 401 has to carry the same reconnect guidance as having no connection
// at all rather than surfacing raw.
//
// As authentication, not as a GitHub reconnect: every Git route this flow calls
// documents its 401 as authentication ("Not authenticated", "Unauthorized -
// invalid or missing token"), and none documents one for the stored GitHub token
// — "The provider connection must be reconnected" is a 409 on the import routes,
// which this flow never calls. Reporting it as a reconnect sent users to the
// dashboard for a failure only signing in again can fix.
func TestClassifyProviderMapsUnauthorizedToNotAuthenticated(t *testing.T) {
	t.Parallel()
	err := classifyProvider(
		&api.Error{StatusCode: http.StatusUnauthorized, Message: "not authenticated"},
		"failed to do a thing")

	require.ErrorIs(t, err, ErrNotAuthenticated)
	assert.Contains(t, err.Error(), "your CLI session may have expired")
}

func TestOrderByOwnerTriesTheOwningAccountFirst(t *testing.T) {
	t.Parallel()
	installations := []apiclient.GitInstallation{
		{Id: 1, AccountLogin: "acme"},
		{Id: 2, AccountLogin: "Octo"},
		{Id: 3, AccountLogin: "other"},
	}

	ordered := orderByOwner(installations, "octo")
	require.Len(t, ordered, 3)
	assert.Equal(t, int64(2), ordered[0].Id, "the owner's installation is tried first, case-insensitively")
	assert.Equal(t, int64(1), ordered[1].Id, "the rest keep their original order")
	assert.Equal(t, int64(3), ordered[2].Id)
}

func TestOrderByOwnerKeepsEveryInstallationWhenNoneMatches(t *testing.T) {
	t.Parallel()
	installations := []apiclient.GitInstallation{{Id: 1, AccountLogin: "acme"}, {Id: 2, AccountLogin: "other"}}

	ordered := orderByOwner(installations, "octo")
	assert.Equal(t, installations, ordered)
}

// On a route whose contract defines a 503, that status means the API has no
// GitHub App configured at all — a different conversation from whatever call
// happened to hit it.
func TestClassifyProviderMapsUnavailableToProviderNotConfigured(t *testing.T) {
	t.Parallel()
	err := classifyProvider(&api.Error{StatusCode: http.StatusServiceUnavailable, Message: "not configured"}, "failed to do a thing")
	require.ErrorIs(t, err, ErrProviderNotConfigured)
}

// On a route whose contract defines no 503 — the project's own binding routes
// only read the database — a 503 came from something in front of the API.
// Claiming the GitHub App is unconfigured there would be a guess, and would
// send the user off to fix something with nothing wrong with it.
func TestClassifyLeavesUnavailableAloneOnRoutesWithoutA503(t *testing.T) {
	t.Parallel()
	cause := &api.Error{StatusCode: http.StatusServiceUnavailable, Message: "upstream unavailable"}
	err := classify(cause, "failed to do a thing")

	require.NotErrorIs(t, err, ErrProviderNotConfigured)
	require.ErrorIs(t, err, cause)
}

func TestClassifyAnnotatesEverythingElse(t *testing.T) {
	t.Parallel()
	cause := errors.New("boom")
	err := classify(cause, "failed to do a thing")

	require.ErrorIs(t, err, cause)
	assert.Equal(t, "failed to do a thing: boom", err.Error())
}
