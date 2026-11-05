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

// A refusal the CLI has no words for keeps the API's words and status.
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

// enable says what LavaFlow does, in the approved AI disclosure, and where it
// sends the project's data before it changes anything, --yes included, on
// stderr so --json stays JSON.
func TestEnableSaysWhatLavaFlowDoesAndWhereTheDataGoesEvenWithYes(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""), "enable", "--yes", "--json")
	require.NoError(t, err)
	assert.Equal(t, "LavaFlow uses AI to diagnose this project's errors from their logs and deployed source, "+
		"and opens fix pull requests in your connected repository.\n"+
		"LavaFlow sends this project's logs and source code to an AI service that Volcano operates.\n", errOut)
	assert.Equal(t, true, decodeJSON(t, out)["enabled"])
	assert.Equal(t, map[string]any{"enabled": true}, fake.lastUpdate(t))
}

func TestEnableAsksFirst(t *testing.T) {
	for answer, wantOn := range map[string]bool{"y\n": true, "yes\n": true, "n\n": false, "\n": false} {
		fake, deps := newLavaflowServer(t)

		out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(answer), "enable")
		require.NoError(t, err, "%q", answer)
		assert.True(t, strings.HasPrefix(errOut, clilavaflow.EnableDisclosure+"\nTurn LavaFlow on? "), "%q: %q", answer, errOut)
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
	assert.Equal(t, clilavaflow.EnableDisclosure+"\n\nstdin is not a terminal: confirmation required; pass --yes", err.Error())
	assert.Empty(t, fake.updates)
}

// Only the project's owner can change LavaFlow. When the settings say the
// caller can't, the change is refused before anything is asked or sent.
func TestChangesBySomeoneWhoIsntTheOwnerAreRefusedFirst(t *testing.T) {
	for _, args := range [][]string{{"enable"}, {"disable"}, {"settings", "set", "--autonomy", "open_pr"}} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.settings["can_update"] = false })

		_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"), args...)
		require.ErrorIs(t, err, clilavaflow.ErrOwnerDecides, "%q", args)
		assert.Equal(t, "only the project's owner can change LavaFlow's settings, "+
			"from their own session while their account is active", err.Error())
		assert.Equal(t, http.StatusForbidden, api.Status(err), "the exit code reports a permission refused")
		assert.NotContains(t, errOut, clilavaflow.EnableNotice, "nothing is asked")
		assert.Empty(t, fake.updates)
	}
}

// The API's refusal of a change is read by its code, not its status or its
// words. A change's 403 without a code covers a read-only token or account as
// well as a plan that couldn't be read, so it says all three and keeps the
// API's words.
func TestARefusedChangeSaysWhy(t *testing.T) {
	for code, want := range map[string]string{
		"person_required": "LavaFlow's settings weren't changed: " + apiPlanRefusal,
		"plan_required":   lavaflowPlanRequired,
		"": "LavaFlow's settings weren't changed: a read-only access token or account can't change them, " +
			"and otherwise the project owner's plan couldn't be read; try again later. Volcano said: \"" + apiPlanRefusal + "\"",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = http.StatusForbidden, http.MethodPut
			s.refusal = map[string]any{"error": apiPlanRefusal}
			if code != "" {
				s.refusal["code"] = code
			}
		})

		_, err := executeLavaflowCommand(t, New(deps), "enable", "--yes")
		require.Error(t, err, code)
		assert.Contains(t, err.Error(), want, code)
		assert.Equal(t, http.StatusForbidden, api.Status(err), code)
	}
}

// Volcano refuses a change that needs a person with person_required, in its
// own words, which say how to sign in instead; without words, the CLI says
// how.
func TestAPersonRequiredRefusalKeepsVolcanosWords(t *testing.T) {
	for _, args := range [][]string{{"enable", "--yes"}, {"settings", "set", "--autonomy", "diagnose_only", "--json"}} {
		for message, want := range map[string]string{
			tokenNeverExpiresRefusal: "LavaFlow's settings weren't changed: " + tokenNeverExpiresRefusal,
			signInRefusal:            "LavaFlow's settings weren't changed: " + signInRefusal,
			"": "changing LavaFlow's settings needs a person's session: a project access token can't. " +
				"Sign in with 'volcano login' and run this again",
		} {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.refuse, s.refuseMethod = http.StatusForbidden, http.MethodPut
				s.refusal = map[string]any{"error": message, "code": "person_required"}
			})

			_, err := executeLavaflowCommand(t, New(deps), args...)
			require.Error(t, err, "%q", args)
			assert.Equal(t, want, err.Error(), "%q", args)
			assert.Equal(t, http.StatusForbidden, api.Status(err), "%q", args)
		}
	}
}

// A token in VOLCANO_TOKEN is used instead of a sign-in, so when Volcano asks
// for one, the refusal says to unset it.
func TestAPersonRequiredRefusalSaysToUnsetVolcanoToken(t *testing.T) {
	for _, message := range []string{tokenNeverExpiresRefusal, signInRefusal} {
		t.Run(message, func(t *testing.T) {
			fake, deps := newLavaflowServer(t)
			fake.set(func(s *lavaflowServer) {
				s.refuse, s.refuseMethod = http.StatusForbidden, http.MethodPut
				s.refusal = map[string]any{"error": message, "code": "person_required"}
			})
			t.Setenv("VOLCANO_TOKEN", "pk-secret")

			_, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--autonomy", "merge", "--yes")
			require.Error(t, err)
			assert.Equal(t, "LavaFlow's settings weren't changed: "+message+". VOLCANO_TOKEN is set, and its token is "+
				"used instead of a 'volcano login' sign-in until you unset it", err.Error())
			assert.Equal(t, http.StatusForbidden, api.Status(err))
		})
	}
}

// Volcano's words for a platform token that never expires, and for a
// credential that isn't the owner's own sign-in, such as one that ended.
const (
	tokenNeverExpiresRefusal = "this token never expires; sign in with `volcano login` to turn this on"
	signInRefusal            = "only the project's owner, signed in with `volcano login`, can turn this on"
)

// An owner's account can turn read-only after settings set reads can_update.
// The change's 403 without a code then says so, with the API's words, rather
// than blame the plan, and keeps the exit code of a permission refused.
func TestAChangeRefusedForAReadOnlyAccountSaysSo(t *testing.T) {
	for _, args := range [][]string{{"enable", "--yes"}, {"disable"}, {"settings", "set", "--autonomy", "diagnose_only"}} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = http.StatusForbidden, http.MethodPut
			s.refusal = map[string]any{"error": "the account is read-only"}
		})

		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.Error(t, err, "%q", args)
		assert.Equal(t, "LavaFlow's settings weren't changed: a read-only access token or account can't change them, "+
			"and otherwise the project owner's plan couldn't be read; try again later. Volcano said: \"the account is read-only\"",
			err.Error(), "%q", args)
		assert.Equal(t, http.StatusForbidden, api.Status(err), "%q", args)
	}
}

// lavaflowPlanUnreadable is what every command says for a route's 403 without
// a code: the owner's plan couldn't be read.
const lavaflowPlanUnreadable = "LavaFlow couldn't read the project owner's plan; try again later"

// apiPlanRefusal is the API's text for a plan refusal. It sends the same text
// with no code when it couldn't read the plan.
const apiPlanRefusal = "the project owner's plan does not include LavaFlow"

// A 403 without a code, for an owner's plan that couldn't be read, says so
// from every route, reads included, rather than repeat the API's plan
// refusal, with the exit code of a permission refused. A start sends no read
// first, and its 403 without a code covers a read-only account too, so it
// says both; TestRunStartSaysWhatToDoAboutARefusal covers it.
func TestAPlanThatCantBeReadSaysSo(t *testing.T) {
	for _, args := range lavaflowCommandArgs() {
		if args[0] == "run" {
			continue
		}
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse = http.StatusForbidden
			s.refusal = map[string]any{"error": apiPlanRefusal}
		})

		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.Error(t, err, "%q", args)
		assert.Equal(t, lavaflowPlanUnreadable, err.Error(), "%q", args)
		assert.Equal(t, http.StatusForbidden, api.Status(err), "%q", args)
		assert.Empty(t, fake.updates, "%q", args)
	}
}
