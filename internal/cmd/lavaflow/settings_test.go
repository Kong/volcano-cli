package lavaflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		{nil, "nothing to change: pass --autonomy, --custom-context, --system-prompt, or --allow-ungated-workflows"},
		{[]string{"--autonomy", "merge"}, `invalid autonomy "merge": use diagnose_only or open_pr`},
		{[]string{"--allow-ungated-workflows", "yes"}, `invalid --allow-ungated-workflows "yes": use true or false`},
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

// settings set --help says what allowing ungated workflows does, that it
// applies to the current repository only, and what it risks.
func TestSettingsSetHelpSaysWhatUngatedWorkflowsRisk(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "--allow-ungated-workflows true: "+clilavaflow.UngatedWorkflowsDisclosure+"\n")
	assert.Contains(t, clilavaflow.UngatedWorkflowsDisclosure, "It applies only to the repository the project points at now.")
	assert.Contains(t, out, "in the current repository: true or false")
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
		assert.Contains(t, errOut, clilavaflow.UngatedWorkflowsDisclosure, "%q", answer)
		assert.Contains(t, errOut, "Allow ungated workflows?", "%q", answer)
		if !wantSaved {
			assert.Empty(t, fake.updates, "%q", answer)
			assert.Empty(t, out, "%q", answer)
			continue
		}
		assert.Equal(t, map[string]any{"allow_ungated_workflows": true}, fake.lastUpdate(t))
		assert.Contains(t, out, "Ungated workflows: allowed in the current repository\n")
	}

	fake, deps := newLavaflowServer(t)
	out, errOut, err := executeLavaflowCommandSplit(t, New(deps), strings.NewReader(""),
		"settings", "set", "--allow-ungated-workflows", "true", "--yes", "--json")
	require.NoError(t, err)
	assert.Contains(t, errOut, clilavaflow.UngatedWorkflowsDisclosure, "--yes still says what it risks")
	assert.NotContains(t, errOut, "Allow ungated workflows?")
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
