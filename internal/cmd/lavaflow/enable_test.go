package lavaflow

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

// enable and disable send enabled alone, so the other settings keep their
// values.
func TestEnableAndDisableChangeOnlyWhetherLavaFlowIsOn(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["autonomy"] = "diagnose_only" })

	out, err := executeLavaflowCommand(t, New(deps), "enable", "--yes")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"enabled": true}, fake.lastUpdate(t))
	assert.Contains(t, out, "LavaFlow is on")
	assert.Contains(t, out, "Autonomy: diagnose only")
	assertLavaFlowWording(t, out)

	out, err = executeLavaflowCommand(t, New(deps), "disable")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"enabled": false}, fake.lastUpdate(t))
	assert.Contains(t, out, "LavaFlow is off. Its runs and records stay readable.")
}

func TestEnableJSONIsTheSavedSettings(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""), "enable", "--yes", "--json")
	require.NoError(t, err)
	value := decodeJSON(t, out)
	assert.Equal(t, true, value["enabled"])
	assert.Equal(t, "open_pr", value["autonomy"])
}

// Changing LavaFlow is a person's decision, so a project access token is
// refused before anything is sent, as a permission the API refuses. Signing
// in saves a token, which a token in VOLCANO_TOKEN still overrides, so that
// case says to unset it first.
func TestChangesWithAProjectTokenNeedAPersonsSession(t *testing.T) {
	for name, tc := range map[string]struct {
		token func(t *testing.T)
		want  string
	}{
		"from the environment": {
			func(t *testing.T) {
				t.Helper()
				t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")
			},
			"changing LavaFlow's settings needs a person's session: a project access token can't. " +
				"Unset VOLCANO_TOKEN and sign in with 'volcano login', then run this again",
		},
		"saved": {
			func(t *testing.T) {
				t.Helper()
				require.NoError(t, (&cliconfig.Config{
					UserToken:      cliconfig.ProjectTokenPrefix + "secret",
					CurrentProject: &cliconfig.ProjectConfig{ID: lavaflowProjectID, Name: "Shop"},
				}).Save())
			},
			"changing LavaFlow's settings needs a person's session: a project access token can't. " +
				"Sign in with 'volcano login' and run this again",
		},
	} {
		for _, args := range [][]string{{"enable"}, {"disable"}, {"settings", "set", "--autonomy", "open_pr"}} {
			t.Run(name+" "+args[0], func(t *testing.T) {
				fake, deps := newLavaflowServer(t)
				tc.token(t)

				_, err := executeLavaflowCommand(t, New(deps), args...)
				require.ErrorIs(t, err, clilavaflow.ErrPersonDecides)
				require.ErrorIs(t, err, cliconfig.ErrAccountTokenRequired, "the exit code reports a permission refused")
				assert.Equal(t, tc.want, err.Error())
				assert.Empty(t, fake.updates)
			})
		}
	}
}

// A project token still reads LavaFlow.
func TestAProjectTokenReadsLavaFlow(t *testing.T) {
	_, deps := newLavaflowServer(t)
	t.Setenv("VOLCANO_TOKEN", cliconfig.ProjectTokenPrefix+"secret")

	for _, args := range [][]string{{"status"}, {"settings"}, {"stats"}, {"resources"}, {"list"}} {
		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.NoError(t, err, "%q", args)
	}
}

// A person without the role that changes settings is refused by the API,
// whose words and status come through.
func TestEnablePassesTheAPIsRefusalThrough(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse = http.StatusForbidden
		s.refuseMethod = http.MethodPut
		s.refusal = map[string]any{"error": "you can't change this project's settings", "code": "forbidden"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "enable", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to turn LavaFlow on")
	assert.Contains(t, err.Error(), "you can't change this project's settings")
	assert.NotContains(t, err.Error(), "isn't available")
	assert.Equal(t, http.StatusForbidden, api.Status(err))
}

// enable says where LavaFlow sends the project's data before it changes
// anything, --yes included, on stderr so --json stays JSON.
func TestEnableSaysWhereTheDataGoesEvenWithYes(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""), "enable", "--yes", "--json")
	require.NoError(t, err)
	assert.Equal(t, clilavaflow.EnableNotice+"\n", errOut)
	assert.Equal(t, "LavaFlow sends this resource's logs and source code to an AI service that Volcano operates.",
		clilavaflow.EnableNotice, "the approved wording, word for word")
	assert.Equal(t, true, decodeJSON(t, out)["enabled"])
	assert.Equal(t, map[string]any{"enabled": true}, fake.lastUpdate(t))
}

func TestEnableAsksFirst(t *testing.T) {
	for answer, wantOn := range map[string]bool{"y\n": true, "yes\n": true, "n\n": false, "\n": false} {
		fake, deps := newLavaflowServer(t)

		out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(answer), "enable")
		require.NoError(t, err, "%q", answer)
		assert.True(t, strings.HasPrefix(errOut, clilavaflow.EnableNotice+"\nTurn LavaFlow on? "), "%q: %q", answer, errOut)
		if wantOn {
			assert.Equal(t, map[string]any{"enabled": true}, fake.lastUpdate(t), "%q", answer)
			assert.Contains(t, out, "LavaFlow is on", "%q", answer)
		} else {
			assert.Empty(t, fake.updates, "%q", answer)
			assert.Contains(t, errOut, "Cancelled.", "%q", answer)
			assert.Empty(t, out, "%q", answer)
		}
	}
}

// Nothing can answer a prompt on a stdin that isn't a terminal, so enable
// refuses rather than read that as a no and exit 0.
func TestEnableWithoutATerminalNeedsYes(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	path := filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(path, []byte("y\n"), 0o600))
	stdin, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stdin.Close() })

	_, _, err = executeLavaflowCommandSplit(t, New(deps), stdin, "enable")
	require.Error(t, err)
	assert.Equal(t, clilavaflow.EnableNotice+"\n\nstdin is not a terminal: confirmation required; pass --yes", err.Error())
	assert.Empty(t, fake.updates)
}
