package lavaflow

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	clilavaflow "github.com/Kong/volcano-cli/internal/lavaflow"
)

func TestSettingsShowsTheTextInFull(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.settings["custom_context"] = "Checkout runs on Fridays only.\nIgnore the legacy cart."
	})

	out, err := executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.Contains(t, out, "LavaFlow: off")
	assert.Contains(t, out, "Ungated workflows: not allowed\n")
	assert.Contains(t, out, "Custom context\n  Checkout runs on Fridays only.\n  Ignore the legacy cart.\n")
	assert.Contains(t, out, "System prompt\n  (not set)\n")
	assertLavaFlowWording(t, out)
}

// set sends only the settings its flags name, so the rest keep their values.
func TestSettingsSetSendsOnlyWhatItChanges(t *testing.T) {
	dir := t.TempDir()
	context := filepath.Join(dir, "context.md")
	require.NoError(t, os.WriteFile(context, []byte("From a file.\n"), 0o600))

	for _, tc := range []struct {
		args []string
		want map[string]any
	}{
		{[]string{"--autonomy", "diagnose_only"}, map[string]any{"autonomy": "diagnose_only"}},
		{[]string{"--system-prompt", "Prefer small fixes."}, map[string]any{"system_prompt": "Prefer small fixes."}},
		{[]string{"--custom-context-file", context}, map[string]any{"custom_context": "From a file.\n"}},
		{[]string{"--custom-context", ""}, map[string]any{"custom_context": ""}},
		{
			[]string{"--autonomy", "open_pr", "--system-prompt", "x", "--custom-context", "y"},
			map[string]any{"autonomy": "open_pr", "system_prompt": "x", "custom_context": "y"},
		},
	} {
		fake, deps := newLavaflowServer(t)

		out, err := executeLavaflowCommand(t, New(deps), append([]string{"settings", "set"}, tc.args...)...)
		require.NoError(t, err, "%q", tc.args)
		assert.Equal(t, tc.want, fake.lastUpdate(t), "%q", tc.args)
		assert.Contains(t, out, "LavaFlow's settings are saved", "%q", tc.args)
	}
}

func TestSettingsSetRefusesBeforeSending(t *testing.T) {
	tooLong := strings.Repeat("é", clilavaflow.MaxTextLength+1)
	for _, tc := range []struct {
		args []string
		want string
	}{
		{nil, "nothing to change: pass --autonomy, --human-review-path, --clear-human-review-paths, " +
			"--custom-context, --system-prompt, or --allow-ungated-workflows"},
		{[]string{"--allow-ungated-workflows", "yes"}, `invalid --allow-ungated-workflows "yes": use true or false`},
		{[]string{"--clear-human-review-paths=false"}, "nothing to change"},
		{[]string{"--autonomy", "always"}, `invalid autonomy "always": use diagnose_only, open_pr or merge`},
		{[]string{"--human-review-path", ""}, `invalid human review path "": use 1 to 200 characters`},
		{[]string{"--human-review-path", strings.Repeat("a", 201)}, "use 1 to 200 characters"},
		{tooManyReviewPaths(), "51 human review paths: the most is 50"},
		{[]string{"--human-review-path", "a", "--clear-human-review-paths"}, "none of the others can be"},
		{[]string{"--merge-mode", "auto"}, "unknown flag: --merge-mode"},
		{[]string{"--system-prompt", tooLong}, "the system prompt is 4001 characters long: the most is 4000"},
		{[]string{"--custom-context-file", filepath.Join(t.TempDir(), "missing.md")}, "failed to read"},
		{[]string{"--custom-context", "a", "--custom-context-file", "b"}, "none of the others can be"},
	} {
		fake, deps := newLavaflowServer(t)

		_, err := executeLavaflowCommand(t, New(deps), append([]string{"settings", "set"}, tc.args...)...)
		require.Error(t, err, "%q", tc.args)
		assert.Contains(t, err.Error(), tc.want, "%q", tc.args)
		assert.Empty(t, fake.updates, "%q", tc.args)
	}
}

// The limit counts characters, as the API does, not bytes.
func TestSettingsSetTakesTheLongestText(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	longest := strings.Repeat("é", clilavaflow.MaxTextLength)

	_, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--custom-context", longest)
	require.NoError(t, err)
	assert.Equal(t, longest, fake.lastUpdate(t)["custom_context"])
}

// settings reads the settings alone: not the capabilities or the repository
// connection status shows.
func TestSettingsReadsOnlyTheSettings(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	_, err := executeLavaflowCommand(t, New(deps), "settings", "--json")
	require.NoError(t, err)
	assert.Zero(t, fake.repoReads)
	assert.Len(t, fake.queries["/settings"], 1)
}

// The examples run the command in the tree it's in, so copying one works.
func TestSettingsSetExamplesRunTheCloudCommand(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "  volcano cloud lavaflow settings set --autonomy diagnose_only\n")
	assert.Contains(t, out, "  volcano cloud lavaflow settings set --custom-context-file lavaflow-context.md\n")
}

func tooManyReviewPaths() []string {
	args := make([]string, 0, 2*(clilavaflow.MaxHumanReviewPaths+1))
	for i := range clilavaflow.MaxHumanReviewPaths + 1 {
		args = append(args, "--human-review-path", fmt.Sprintf("infra/%d/**", i))
	}
	return args
}

// settings set --help says what autonomy merge does, in the approved words,
// and that --yes skips the question.
func TestSettingsSetHelpSaysWhatMergingDoes(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "merge\nis for a project where merging is available. "+clilavaflow.MergeDisclosure+
		"\n--autonomy merge prints that and asks first; --yes skips the question.")
	assert.Contains(t, out, "--human-review-path names a path whose changes always hold a fix for a")
	merging, _, _ := strings.Cut(out, "\n--allow-ungated-workflows")
	assert.NotContains(t, merging, "approv")
}

// --autonomy merge says what merging does and asks first, and changes
// nothing on a no.
func TestSettingsSetAsksBeforeMerging(t *testing.T) {
	for answer, wantMerge := range map[string]bool{"y\n": true, "n\n": false} {
		fake, deps := newLavaflowServer(t)

		out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(answer),
			"settings", "set", "--autonomy", "merge")
		require.NoError(t, err, "%q", answer)
		assert.Contains(t, errOut, clilavaflow.MergeDisclosure, "%q", answer)
		assert.Contains(t, errOut, "Let LavaFlow merge fixes?", "%q", answer)
		if !wantMerge {
			assert.Empty(t, fake.updates, "%q", answer)
			assert.Empty(t, out, "%q", answer)
			continue
		}
		assert.Equal(t, map[string]any{"autonomy": "merge"}, fake.lastUpdate(t))
		assert.Contains(t, out, "Autonomy: open fix pull requests and may merge them")
	}
}

// --yes merges without asking, but still says what merging does, on stderr,
// so --json output stays JSON.
func TestSettingsSetMergesWithYesAndJSON(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"settings", "set", "--autonomy", "merge", "--yes", "--json")
	require.NoError(t, err)
	assert.Equal(t, clilavaflow.MergeDisclosure+"\n", errOut)
	assert.Contains(t, errOut, "Turning on merge lets LavaFlow's fixes, including those from runs it starts on its own, "+
		"run your CI with the secrets your pull request workflows get.", "--yes still says what it risks")
	assert.Equal(t, "merge", decodeJSON(t, out)["autonomy"])
	assert.Equal(t, map[string]any{"autonomy": "merge"}, fake.lastUpdate(t))
}

// Nothing can answer a prompt on a stdin that isn't a terminal, so
// --autonomy merge refuses without --yes rather than read that as a no.
func TestSettingsSetMergingWithoutATerminalNeedsYes(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	path := filepath.Join(t.TempDir(), "stdin")
	require.NoError(t, os.WriteFile(path, []byte("y\n"), 0o600))
	stdin, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stdin.Close() })

	_, _, err = executeLavaflowCommandSplit(t, New(deps), stdin, "settings", "set", "--autonomy", "merge")
	require.EqualError(t, err, clilavaflow.MergeDisclosure+"\n\nstdin is not a terminal: confirmation required; pass --yes")
	assert.Empty(t, fake.updates)
}

// The human review paths replace the saved ones, or are cleared, and the
// settings print them.
func TestSettingsSetChangesTheHumanReviewPaths(t *testing.T) {
	fake, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "settings", "set",
		"--human-review-path", "infra/**", "--human-review-path", "db/migrations/**")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"merge_policy": map[string]any{"human_review_paths": []any{"infra/**", "db/migrations/**"}}},
		fake.lastUpdate(t))
	assert.Contains(t, out, "Human review paths: infra/**, db/migrations/**")

	out, err = executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.Contains(t, out, "Human review paths: infra/**, db/migrations/**")

	out, err = executeLavaflowCommand(t, New(deps), "settings", "set", "--clear-human-review-paths")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"merge_policy": map[string]any{"human_review_paths": []any{}}}, fake.lastUpdate(t))
	assert.Contains(t, out, "Human review paths: none")
}

// An API without a merge policy prints no human review paths.
func TestSettingsWithoutAMergePolicyPrintsNoPaths(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { delete(s.settings, "merge_policy") })

	out, err := executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.NotContains(t, out, "Human review paths")
}

// When Volcano can't check the owner's push access for autonomy merge, it
// answers 429 or 503, and the CLI says when to try again.
func TestSettingsSetSaysWhenToTryMergingAgain(t *testing.T) {
	for name, tc := range map[string]struct {
		status  int
		headers map[string]string
		want    string
	}{
		"limited":    {http.StatusTooManyRequests, map[string]string{"Retry-After": "30"}, "Try again in 30s"},
		"unanswered": {http.StatusServiceUnavailable, nil, "Try again later"},
		"already passed": {
			http.StatusServiceUnavailable,
			map[string]string{"Retry-After": "Mon, 02 Jan 2006 15:04:05 GMT"},
			"Try again later",
		},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = tc.status, http.MethodPut
			s.refusal = map[string]any{"error": "the push check didn't answer"}
			s.refusalHeaders = tc.headers
		})

		_, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--autonomy", "merge", "--yes")
		require.Error(t, err, name)
		assert.Equal(t, "LavaFlow's settings weren't changed: Volcano couldn't check that the project owner's GitHub "+
			"account can push to the repository. "+tc.want, err.Error(), name)
		assert.Equal(t, tc.status, api.Status(err), name)
	}
}

// A save that kept losing a race with another change answers 409 and saved
// nothing; the CLI says so in Volcano's words.
func TestSettingsSetSaysASaveThatRacedSavedNothing(t *testing.T) {
	const message = "the LavaFlow settings changed while saving; nothing was saved, send the request again"
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusConflict, http.MethodPut
		s.refusal = map[string]any{"error": message}
	})

	_, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--custom-context", "Deploys run on Fridays.")
	require.Error(t, err)
	assert.Equal(t, "failed to change LavaFlow's settings: HTTP 409: "+message, err.Error())
	assert.Equal(t, http.StatusConflict, api.Status(err))
}

// The API refuses autonomy merge while merging is off for the project, and
// with person_required, in its own words, while the owner's GitHub account
// can't push to the repository or the project has none. The CLI says which.
// A token in VOLCANO_TOKEN has nothing to do with the repository, so a
// refusal about it doesn't say to unset the token.
func TestSettingsSetSaysWhyMergingIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		status        int
		message, code string
		want          string
	}{
		"merging off": {
			http.StatusBadRequest, "autonomy merge is not available", "feature_unavailable",
			"LavaFlow merging isn't available for this project yet",
		},
		"owner can't push": {
			http.StatusForbidden, ownerCantPushRefusal, "person_required",
			"LavaFlow's settings weren't changed: " + ownerCantPushRefusal,
		},
		"no repository": {
			http.StatusForbidden, noRepositoryRefusal, "person_required",
			"LavaFlow's settings weren't changed: " + noRepositoryRefusal,
		},
	} {
		for _, envToken := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s, VOLCANO_TOKEN %t", name, envToken), func(t *testing.T) {
				fake, deps := newLavaflowServer(t)
				fake.set(func(s *lavaflowServer) {
					s.refuse, s.refuseMethod = tc.status, http.MethodPut
					s.refusal = map[string]any{"error": tc.message, "code": tc.code}
				})
				if envToken {
					t.Setenv("VOLCANO_TOKEN", "pk-secret")
				}

				_, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--autonomy", "merge", "--yes")
				require.Error(t, err)
				assert.Equal(t, tc.want, err.Error())
				assert.Equal(t, tc.status, api.Status(err))
			})
		}
	}
}

// Volcano's words when the owner's GitHub account can't push to the
// project's repository, and when the project has none.
const (
	ownerCantPushRefusal = "give the project owner's GitHub account push access to the project's repository, " +
		"then turn this on"
	noRepositoryRefusal = "connect the project to a GitHub repository its owner can push to, then turn this on"
)

// Where the project's capabilities say LavaFlow is available but merging
// isn't, --autonomy merge says so before it warns or asks, --yes included,
// and sends nothing, with the exit code the save's refusal has.
func TestSettingsSetSaysMergingIsntAvailableBeforeAsking(t *testing.T) {
	for _, tc := range []struct {
		capability string
		args       []string
	}{
		{"unavailable", nil},
		{"limited", nil},
		{"unavailable", []string{"--yes", "--json"}},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.mergeCapability = tc.capability })

		_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"),
			append([]string{"settings", "set", "--autonomy", "merge"}, tc.args...)...)
		require.ErrorIs(t, err, clilavaflow.ErrMergingNotAvailable, "%q %q", tc.capability, tc.args)
		assert.Equal(t, "LavaFlow merging isn't available for this project yet", err.Error())
		assert.Equal(t, http.StatusBadRequest, api.Status(err))
		assert.NotContains(t, errOut, clilavaflow.MergeWarning, "nothing is said")
		assert.NotContains(t, errOut, "Let LavaFlow merge fixes?", "nothing is asked")
		assert.Empty(t, fake.updates)
	}
}

// Where merging is available, --autonomy merge asks as before.
func TestSettingsSetAsksWhereMergingIsAvailable(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.mergeCapability = "available" })

	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"),
		"settings", "set", "--autonomy", "merge")
	require.NoError(t, err)
	assert.Contains(t, errOut, clilavaflow.MergeDisclosure)
	assert.Contains(t, errOut, "Let LavaFlow merge fixes?")
	assert.Equal(t, map[string]any{"autonomy": "merge"}, fake.lastUpdate(t))
}

// When the capabilities can't be read or don't list merging, --autonomy merge
// asks as before and the save decides. Where LavaFlow itself isn't available,
// the save refuses with LavaFlow's own reason and exit code.
func TestSettingsSetLeavesMergingToTheSaveWhenTheCapabilitiesDontSay(t *testing.T) {
	for name, tc := range map[string]struct {
		server func(*lavaflowServer)
		want   string
		status int
	}{
		"unreadable": {func(s *lavaflowServer) {
			s.mergeCapability, s.capabilitiesFailure = "unavailable", http.StatusInternalServerError
			s.refuse, s.refuseMethod = http.StatusBadRequest, http.MethodPut
			s.refusal = map[string]any{"error": "autonomy merge is not available", "code": "feature_unavailable"}
		}, "LavaFlow merging isn't available for this project yet", http.StatusBadRequest},
		"not listed": {func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = http.StatusBadRequest, http.MethodPut
			s.refusal = map[string]any{"error": "autonomy merge is not available", "code": "feature_unavailable"}
		}, "LavaFlow merging isn't available for this project yet", http.StatusBadRequest},
		"LavaFlow unavailable": {func(s *lavaflowServer) {
			s.capability, s.mergeCapability, s.gateOff = "read_only", "unavailable", true
		}, lavaflowNotAvailable, http.StatusNotFound},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(tc.server)

		_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"),
			"settings", "set", "--autonomy", "merge")
		require.Error(t, err, name)
		assert.Equal(t, tc.want, err.Error(), name)
		assert.Equal(t, tc.status, api.Status(err), name)
		assert.Contains(t, errOut, "Let LavaFlow merge fixes?", "%s: it asked, so the save refused", name)
	}
}

// A project already at merge sends merge again, as an idempotent apply does,
// while merging isn't available: the save accepts the saved autonomy, so the
// CLI doesn't refuse it first.
func TestSettingsSetResendsMergeWhileMergingIsntAvailable(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["autonomy"], s.mergeCapability = "merge", "unavailable" })

	_, _, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"settings", "set", "--autonomy", "merge", "--custom-context", "Checkout runs on Fridays.", "--yes")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"autonomy": "merge", "custom_context": "Checkout runs on Fridays."}, fake.lastUpdate(t))
}

// Human review paths are part of merging, so Volcano refuses them where
// merging isn't available, and the CLI says so.
func TestSettingsSetSaysReviewPathsNeedMerging(t *testing.T) {
	for _, args := range [][]string{
		{"settings", "set", "--human-review-path", "db/migrations/**"},
		{"settings", "set", "--clear-human-review-paths"},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = http.StatusBadRequest, http.MethodPut
			s.refusal = map[string]any{"error": "merging is not available", "code": "feature_unavailable"}
		})

		_, err := executeLavaflowCommand(t, New(deps), args...)
		require.Error(t, err, "%q", args)
		assert.Equal(t, "LavaFlow merging isn't available for this project yet", err.Error(), "%q", args)
	}
}

// settings set --help says what allowing ungated workflows does, who turns
// it on and off, that it applies to the connected repository only, and what it
// risks, in the API contract's own sentences.
func TestSettingsSetHelpSaysWhatUngatedWorkflowsRisk(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "\n--allow-ungated-workflows: "+clilavaflow.UngatedWorkflowsSetting+" "+
		clilavaflow.UngatedWorkflowsRules+"\n")
	assert.Contains(t, out, "\n--allow-ungated-workflows true: "+clilavaflow.UngatedWorkflowsDisclosure+
		"\nIt prints that and asks first; --yes skips the question.")
	assert.Contains(t, out, clilavaflow.UngatedWorkflowsSetting+" Pass true or false.")

	for _, sentence := range []string{
		"Whether LavaFlow opens a fix's pull request even where a workflow could run it, or put its text into a " +
			"command, with the repository's secrets before a person approves it.",
		"Off by default; while off, such a fix stays on its run and no pull request opens.",
		"Only the project's owner turns it on, signed in as a person: with a Volcano sign-in session, the " +
			"platform token `volcano login` stores, or another platform token that expires.",
		"It applies to the repository their GitHub account can push to.",
		"Any of the owner's credentials turns it off.",
		"Turns on opening fixes past ungated workflows, for the repository the project is connected to now; it " +
			"applies only to that repository.",
		"Connecting the project to another repository, or disconnecting it, turns it off, and connecting the " +
			"first one again doesn't turn it back on.",
		clilavaflow.UngatedWorkflowsWarning,
	} {
		assert.Contains(t, out, sentence)
		assert.Contains(t, ungatedWorkflowsContract(t), sentence, "the help says it as the API's contract does")
	}
}

// ungatedWorkflowsContract is every description the vendored API contract
// gives the ungated workflows setting, with YAML's line folding undone.
func ungatedWorkflowsContract(t *testing.T) string {
	t.Helper()
	spec, err := os.ReadFile(filepath.Join("..", "..", "..", "openapi", "openapi.yaml"))
	require.NoError(t, err)
	var descriptions []string
	lines := strings.Split(string(spec), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "allow_ungated_workflows:" || i+2 >= len(lines) {
			continue
		}
		description, ok := strings.CutPrefix(strings.TrimSpace(lines[i+2]), "description: ")
		if !ok {
			continue
		}
		descriptions = append(descriptions, strings.ReplaceAll(strings.Trim(description, "'"), "''", "'"))
	}
	require.Len(t, descriptions, 2, "LavaflowSettings and LavaflowSettingsUpdate describe allow_ungated_workflows")
	return strings.Join(descriptions, "\n")
}

// Allowing ungated workflows says what it risks, in the approved words, and
// asks first: only a yes or --yes sends it, and nothing that can't answer is
// read as a yes. The warning goes to stderr, so --json output stays JSON.
// Turning it off asks nothing.
func TestSettingsSetAllowsUngatedWorkflowsOnlyAfterAsking(t *testing.T) {
	for answer, wantSaved := range map[string]bool{"y\n": true, "n\n": false} {
		fake, deps := newLavaflowServer(t)

		out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(answer),
			"settings", "set", "--allow-ungated-workflows", "true")
		require.NoError(t, err, "%q", answer)
		assert.Contains(t, errOut, ungatedWorkflowsDisclosure, "%q", answer)
		assert.Contains(t, errOut, "Allow ungated workflows?", "%q", answer)
		if !wantSaved {
			assert.Empty(t, fake.updates, "%q", answer)
			assert.Empty(t, out, "%q", answer)
			continue
		}
		assert.Equal(t, map[string]any{"allow_ungated_workflows": true}, fake.lastUpdate(t))
		assert.Contains(t, out, "Ungated workflows: allowed for the connected repository\n")
	}

	fake, deps := newLavaflowServer(t)
	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"settings", "set", "--allow-ungated-workflows", "true", "--yes", "--json")
	require.NoError(t, err)
	assert.Equal(t, ungatedWorkflowsDisclosure+"\n", errOut, "--yes still says what it risks, and only that")
	assert.Equal(t, true, decodeJSON(t, out)["allow_ungated_workflows"])
	assert.Equal(t, map[string]any{"allow_ungated_workflows": true}, fake.lastUpdate(t))

	stdin, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = stdin.Close() })
	fake, deps = newLavaflowServer(t)
	_, _, err = executeLavaflowCommandSplit(t, New(deps), stdin, "settings", "set", "--allow-ungated-workflows", "true")
	require.ErrorContains(t, err, "stdin is not a terminal: confirmation required; pass --yes")
	assert.Empty(t, fake.updates)

	fake, deps = newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["allow_ungated_workflows"] = true })
	out, errOut, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"settings", "set", "--allow-ungated-workflows", "false")
	require.NoError(t, err)
	assert.Empty(t, errOut, "turning it off asks nothing")
	assert.Equal(t, map[string]any{"allow_ungated_workflows": false}, fake.lastUpdate(t))
	assert.Contains(t, out, "Ungated workflows: not allowed\n")

	out, err = executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.Contains(t, out, "Ungated workflows: not allowed\n")
}

// While LavaFlow isn't available, Volcano turns ungated workflows off only
// with LavaFlow itself, so the CLI sends both and says so. A capabilities read
// that fails leaves the save to say why.
func TestSettingsSetTurnsOffUngatedWorkflowsWhileLavaFlowIsntAvailable(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.capability, s.gateOff = "read_only", true
		s.settings["enabled"], s.settings["allow_ungated_workflows"] = true, true
	})
	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"settings", "set", "--allow-ungated-workflows", "false", "--json")
	require.NoError(t, err)
	assert.Equal(t, ungatedWorkflowsOffTurnsLavaFlowOff+"\n", errOut)
	assert.Equal(t, map[string]any{"enabled": false, "allow_ungated_workflows": false}, fake.lastUpdate(t))
	saved := decodeJSON(t, out)
	assert.Equal(t, false, saved["enabled"])
	assert.Equal(t, false, saved["allow_ungated_workflows"])

	fake, deps = newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.capability, s.gateOff, s.capabilitiesFailure = "read_only", true, http.StatusInternalServerError
		s.settings["allow_ungated_workflows"] = true
	})
	_, errOut, err = executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"settings", "set", "--allow-ungated-workflows", "false")
	require.Error(t, err)
	assert.Equal(t, http.StatusNotFound, api.Status(err))
	assert.NotContains(t, errOut, ungatedWorkflowsOffTurnsLavaFlowOff)
	assert.Equal(t, map[string]any{"allow_ungated_workflows": false}, fake.lastUpdate(t))
}

// Turning on merging and ungated workflows in one change says what each does,
// then asks once; a no sends nothing.
func TestSettingsSetAsksOnceAboutMergingAndUngatedWorkflows(t *testing.T) {
	for answer, wantSaved := range map[string]bool{"y\n": true, "n\n": false} {
		fake, deps := newLavaflowServer(t)

		_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(answer),
			"settings", "set", "--autonomy", "merge", "--allow-ungated-workflows", "true")
		require.NoError(t, err, "%q", answer)
		assert.Contains(t, errOut, clilavaflow.MergeDisclosure+"\n"+ungatedWorkflowsDisclosure+"\n", "%q", answer)
		assert.Equal(t, 1, strings.Count(errOut, "Type 'y' or 'yes' to confirm"), "%q", answer)
		assert.Contains(t, errOut, "Let LavaFlow merge fixes, and allow ungated workflows?", "%q", answer)
		if !wantSaved {
			assert.Empty(t, fake.updates, "%q", answer)
			continue
		}
		assert.Equal(t, map[string]any{"autonomy": "merge", "allow_ungated_workflows": true}, fake.lastUpdate(t))
	}

	_, deps := newLavaflowServer(t)
	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"settings", "set", "--autonomy", "merge", "--allow-ungated-workflows", "true", "--yes", "--json")
	require.NoError(t, err)
	assert.Equal(t, clilavaflow.MergeDisclosure+"\n"+ungatedWorkflowsDisclosure+"\n", errOut)
}

// Only the owner allows ungated workflows: someone the settings say can't
// change them is refused before anything is asked.
func TestSettingsSetAsksAboutUngatedWorkflowsOnlyTheOwner(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["can_update"] = false })

	_, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader("y\n"),
		"settings", "set", "--allow-ungated-workflows", "true")
	require.ErrorIs(t, err, clilavaflow.ErrOwnerDecides)
	assert.NotContains(t, errOut, "Allow ungated workflows?")
	assert.Empty(t, fake.updates)
}
