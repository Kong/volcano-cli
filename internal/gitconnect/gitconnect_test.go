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

func TestUsableConnectionsKeepsLiveGitHubConnections(t *testing.T) {
	t.Parallel()
	connections := []apiclient.GitConnection{
		{Provider: "github", Status: "active", ProviderLogin: "octo"},
		{Provider: "github", Status: "revoked", ProviderLogin: "stale"},
		{Provider: "gitlab", Status: "active", ProviderLogin: "elsewhere"},
		// Status is provider-defined text: anything that is not an explicit bad
		// state is treated as usable rather than guessed at.
		{Provider: "GitHub", Status: "", ProviderLogin: "unlabelled"},
	}

	usable := usableConnections(connections)
	require.Len(t, usable, 2)
	assert.Equal(t, "octo", usable[0].ProviderLogin)
	assert.Equal(t, "unlabelled", usable[1].ProviderLogin)
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
