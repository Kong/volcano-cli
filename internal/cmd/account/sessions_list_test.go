package account

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

func TestSessionsListShowsBrowserAndCLISessionsAndMarksThisOne(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)

	out, err := executeAccountCommand(t, api.serve(), "", "sessions", "list")
	require.NoError(t, err)

	assert.Equal(t, []string{"GET /user/sessions"}, api.requests())
	assert.Contains(t, out, currentSessionID+"  CLI       volcano-cli/0.58.0")
	assert.Contains(t, out, browserSessionID+"  Browser   Mozilla/5.0 (Macintosh)")
	assert.Contains(t, out, otherCLISessionID+"  CLI       volcano-cli/0.57.0")
	assert.Contains(t, out, "this CLI")
	assert.Contains(t, out, "Showing 3 of 3 session(s) (page 1, limit 100)")
}

// Any client can name itself anything, so a device string can't move the
// cursor or clear rows of the table.
func TestSessionsListDropsControlCharactersFromDevices(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)
	api.sessions = append(api.sessions, sessionPayload(otherCLISessionID, "email", "spoof\x1b[2K\r\u009b2Kagent", false))

	out, err := executeAccountCommand(t, api.serve(), "", "sessions", "list")
	require.NoError(t, err)

	assert.Contains(t, out, "spoof[2K2Kagent")
	assert.NotContains(t, out, "\x1b")
	assert.NotContains(t, out, "\r")
	assert.NotContains(t, out, "\u009b")
}

func TestSessionsListJSON(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)

	out, err := executeAccountCommand(t, api.serve(), "", "sessions", "list", "--json")
	require.NoError(t, err)

	var page struct {
		Sessions []struct {
			ID        string `json:"id"`
			Provider  string `json:"provider"`
			IsCurrent bool   `json:"is_current"`
		} `json:"sessions"`
		Total int `json:"total"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &page))
	require.Len(t, page.Sessions, 3)
	assert.Equal(t, 3, page.Total)
	assert.Equal(t, currentSessionID, page.Sessions[0].ID)
	assert.True(t, page.Sessions[0].IsCurrent)
	assert.Equal(t, "email", page.Sessions[1].Provider)
}

// The routes take an account token only, so a project access token is refused
// before a request it could only get a 403 for.
func TestSessionsListRefusesAProjectAccessToken(t *testing.T) {
	setAccountTestHome(t, cliconfig.ProjectTokenPrefix+"scoped")
	api := newSessionsAPI(t)

	_, err := executeAccountCommand(t, api.serve(), "", "sessions", "list")
	require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired)
	assert.Empty(t, api.requests())
}

func TestSessionsListNeedsALogin(t *testing.T) {
	setAccountTestHome(t, "")
	api := newSessionsAPI(t)

	_, err := executeAccountCommand(t, api.serve(), "", "sessions", "list")
	require.ErrorIs(t, err, cliconfig.ErrNotAuthenticated)
	assert.Empty(t, api.requests())
}

// An API without the routes answers 404, which says nothing about sessions.
func TestSessionsListOnAnAPIWithoutTheRoutes(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)
	api.status = http.StatusNotFound

	_, err := executeAccountCommand(t, api.serve(), "", "sessions", "list")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this Volcano API can't manage sessions from the CLI")
}
