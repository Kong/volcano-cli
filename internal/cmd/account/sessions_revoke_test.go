package account

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionsRevokeSignsOutOneSession(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)

	out, err := executeAccountCommand(t, api.serve(), "", "sessions", "revoke", browserSessionID)
	require.NoError(t, err)

	assert.Equal(t, []string{"GET /user/sessions", "DELETE /user/sessions/" + browserSessionID}, api.requests())
	assert.Contains(t, out, "Signed out session "+browserSessionID)
}

// Revoking this CLI's own session would leave a saved login the server no
// longer accepts, so it is refused and logout named instead.
func TestSessionsRevokeRefusesThisCLIsOwnSession(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)

	_, err := executeAccountCommand(t, api.serve(), "", "sessions", "revoke", currentSessionID)
	require.Error(t, err)

	assert.Contains(t, err.Error(), "that is the session this CLI is logged in with")
	assert.Contains(t, err.Error(), "run volcano logout")
	assert.Equal(t, []string{"GET /user/sessions"}, api.requests(), "nothing is revoked")
}

func TestSessionsRevokeReportsASessionTheAccountDoesNotHave(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)
	const missing = "55555555-5555-4555-8555-555555555555"

	_, err := executeAccountCommand(t, api.serve(), "", "sessions", "revoke", missing)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no session with that ID on this account: "+missing)
}

func TestSessionsRevokeRefusesWhatCannotNameASession(t *testing.T) {
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"nothing":      {args: nil, want: "name the session to sign out"},
		"both":         {args: []string{browserSessionID, "--all-others"}, want: "pass a session ID or --all-others, not both"},
		"not an ID":    {args: []string{"V7HR"}, want: `"V7HR" is not a session ID`},
		"two sessions": {args: []string{browserSessionID, otherCLISessionID}, want: "accepts at most 1 arg"},
	} {
		t.Run(name, func(t *testing.T) {
			setAccountTestHome(t, accountToken)
			api := newSessionsAPI(t)

			_, err := executeAccountCommand(t, api.serve(), "", append([]string{"sessions", "revoke"}, tc.args...)...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Empty(t, api.requests())
		})
	}
}

func TestSessionsRevokeAllOthersAsksFirstAndSaysBrowsersGoToo(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)

	out, err := executeAccountCommand(t, api.serve(), "y\n", "sessions", "revoke", "--all-others")
	require.NoError(t, err)

	assert.Contains(t, out, "the dashboard in every browser, and every other CLI login. This CLI stays logged in.")
	assert.Contains(t, out, "Sign out every other session?")
	assert.Equal(t, []string{"DELETE /user/sessions"}, api.requests())
	assert.Contains(t, out, "Signed out every other session; this CLI stays logged in")
}

func TestSessionsRevokeAllOthersDeclined(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)

	_, err := executeAccountCommand(t, api.serve(), "n\n", "sessions", "revoke", "--all-others")
	require.NoError(t, err)
	assert.Empty(t, api.requests())
}

func TestSessionsRevokeAllOthersWithYes(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)

	_, err := executeAccountCommand(t, api.serve(), "", "sessions", "revoke", "--all-others", "--yes")
	require.NoError(t, err)
	assert.Equal(t, []string{"DELETE /user/sessions"}, api.requests())
}

// Nobody can answer the prompt in a script, which must not read as a decline
// that exits 0 with every session still signed in.
func TestSessionsRevokeAllOthersWithoutATerminalNeedsYes(t *testing.T) {
	setAccountTestHome(t, accountToken)
	api := newSessionsAPI(t)
	stdin, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = stdin.Close() })

	cmd := New(api.serve())
	cmd.SetIn(stdin)
	cmd.SetOut(new(nopWriter))
	cmd.SetErr(new(nopWriter))
	cmd.SetArgs([]string{"sessions", "revoke", "--all-others"})
	err = cmd.Execute()
	require.Error(t, err)

	assert.Contains(t, err.Error(), "pass --yes")
	assert.Empty(t, api.requests())
}

type nopWriter struct{}

func (*nopWriter) Write(p []byte) (int, error) { return len(p), nil }
