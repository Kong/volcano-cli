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
		{nil, "nothing to change: pass --autonomy, --merge-mode, --custom-context, or --system-prompt"},
		{[]string{"--autonomy", "always"}, `invalid autonomy "always": use diagnose_only, open_pr or merge`},
		{[]string{"--merge-mode", "always"}, `invalid merge mode "always": use human, risk_based or auto`},
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

// --autonomy merge and --merge-mode send only what they change, and
// settings, status and the saved settings say when LavaFlow merges. Below
// autonomy merge the mode is kept, and merging is off. An API that doesn't
// report a merge policy gets no line.
func TestSettingsSetChangesWhenLavaFlowMerges(t *testing.T) {
	for mode, want := range map[string]string{
		"human":      "Merging: a person approves each merge",
		"risk_based": "Merging: a person approves risky fixes; LavaFlow merges the rest once CI passes",
		"auto":       "Merging: LavaFlow merges each reviewed fix once CI passes; changes to sensitive files still wait for a person",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.settings["merge_policy"] = lavaflowMergePolicy("human") })

		out, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--autonomy", "merge", "--merge-mode", mode)
		require.NoError(t, err, mode)
		assert.Equal(t, map[string]any{"autonomy": "merge", "merge_policy": map[string]any{"mode": mode}}, fake.lastUpdate(t), mode)
		assert.Contains(t, out, "Autonomy: open fix pull requests and merge them as the merge mode allows\n", mode)
		assert.Contains(t, out, want+"\n", mode)

		for _, args := range [][]string{{"settings"}, {"status"}} {
			out, err = executeLavaflowCommand(t, New(deps), args...)
			require.NoError(t, err, "%s %q", mode, args)
			assert.Contains(t, out, want+"\n", "%s %q", mode, args)
		}
		fake.set(func(s *lavaflowServer) {
			assert.Equal(t, false, s.settings["merge_policy"].(map[string]any)["allow_merge_without_checks"],
				"a mode change keeps the rest of the policy")
		})
	}

	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["merge_policy"] = lavaflowMergePolicy("human") })
	out, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--merge-mode", "auto")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"merge_policy": map[string]any{"mode": "auto"}}, fake.lastUpdate(t))
	assert.Contains(t, out, "Merging: off (at autonomy merge: LavaFlow merges each reviewed fix once CI passes; "+
		"changes to sensitive files still wait for a person)\n")

	_, deps = newLavaflowServer(t)
	out, err = executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.NotContains(t, out, "Merging")
}

func lavaflowMergePolicy(mode string) map[string]any {
	return map[string]any{
		"mode": mode, "human_review_paths": []any{}, "allow_merge_without_checks": false, "allow_ci_before_approval": false,
	}
}

// While merging is off for the project, the API refuses autonomy merge as a
// value it doesn't take yet, and the CLI says merging isn't available.
func TestSettingsSetSaysWhenMergingIsntAvailable(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusBadRequest, http.MethodPut
		s.refusal = map[string]any{"error": "autonomy merge is not available", "code": "feature_unavailable"}
	})

	_, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--autonomy", "merge")
	require.Error(t, err)
	assert.Equal(t, "LavaFlow merging isn't available for this project yet", err.Error())
	assert.Equal(t, http.StatusBadRequest, api.Status(err))
}
