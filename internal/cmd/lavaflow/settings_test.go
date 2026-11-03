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
	"github.com/Kong/volcano-cli/internal/apiclient"
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
		{nil, "nothing to change: pass --autonomy, --merge-mode, --human-review-path, --clear-human-review-paths, " +
			"--allow-merge-without-checks, --allow-ci-before-approval, --custom-context, or --system-prompt"},
		{[]string{"--autonomy", "always"}, `invalid autonomy "always": use diagnose_only, open_pr or merge`},
		{[]string{"--merge-mode", "always"}, `invalid merge mode "always": use ` + humanMergeMode + `, risk_based or auto`},
		{[]string{"--allow-merge-without-checks", "yes"}, `invalid --allow-merge-without-checks "yes": use true or false`},
		{[]string{"--allow-ci-before-approval", "1"}, `invalid --allow-ci-before-approval "1": use true or false`},
		{[]string{"--human-review-path", ""}, `invalid human review path "": use 1 to 200 characters`},
		{[]string{"--human-review-path", strings.Repeat("a", 201)}, "use 1 to 200 characters"},
		{tooManyReviewPaths(), "51 human review paths: the most is 50"},
		{[]string{"--human-review-path", "a", "--clear-human-review-paths"}, "none of the others can be"},
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
		humanMergeMode: "Merging: A person approves every merge.",
		"risk_based":   "Merging: LavaFlow merges a fix on its own only when the policy rates it low risk, and holds the rest for a person.",
		"auto":         "Merging: LavaFlow merges every fix that passes review and required checks, unless a hold applies (held paths, rules it can't vouch for).",
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) { s.settings["merge_policy"] = lavaflowMergePolicy(humanMergeMode) })

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
	fake.set(func(s *lavaflowServer) { s.settings["merge_policy"] = lavaflowMergePolicy(humanMergeMode) })
	out, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--merge-mode", "auto")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"merge_policy": map[string]any{"mode": "auto"}}, fake.lastUpdate(t))
	assert.Contains(t, out, "Merging: off. At autonomy merge: LavaFlow merges every fix that passes review and required checks, unless a hold applies (held paths, rules it can't vouch for).\n")

	fake, deps = newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { delete(s.settings, "merge_policy") })
	out, err = executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.NotContains(t, out, "Merging")
	assert.NotContains(t, out, "Human review paths")
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

func tooManyReviewPaths() []string {
	args := make([]string, 0, 2*(clilavaflow.MaxHumanReviewPaths+1))
	for i := range clilavaflow.MaxHumanReviewPaths + 1 {
		args = append(args, "--human-review-path", fmt.Sprintf("infra/%d/**", i))
	}
	return args
}

// The merge policy flags send only the fields they name, and settings prints
// the whole policy. A server refusal, such as CI before approval outside
// auto, keeps the API's words.
func TestSettingsSetChangesTheMergePolicy(t *testing.T) {
	fake, deps := newLavaflowServer(t)
	fake.set(func(s *lavaflowServer) { s.settings["merge_policy"] = lavaflowMergePolicy(humanMergeMode) })

	out, err := executeLavaflowCommand(t, New(deps), "settings", "set",
		"--human-review-path", "infra/**", "--human-review-path", "migrations/**")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"merge_policy": map[string]any{"human_review_paths": []any{"infra/**", "migrations/**"}}},
		fake.lastUpdate(t))
	assert.Contains(t, out, "Human review paths: infra/**, migrations/**\n")

	out, err = executeLavaflowCommand(t, New(deps), "settings", "set",
		"--allow-merge-without-checks", "true", "--allow-ci-before-approval", "false")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"merge_policy": map[string]any{
		"allow_merge_without_checks": true, "allow_ci_before_approval": false,
	}}, fake.lastUpdate(t))
	assert.Contains(t, out, "Merge without checks: allowed\n")
	assert.Contains(t, out, "CI before approval: not allowed\n")

	out, err = executeLavaflowCommand(t, New(deps), "settings")
	require.NoError(t, err)
	assert.Contains(t, out, "Merging: off. At autonomy merge: A person approves every merge.\n"+
		"Human review paths: infra/**, migrations/**\n"+
		"Merge without checks: allowed\n"+
		"CI before approval: not allowed\n")

	_, err = executeLavaflowCommand(t, New(deps), "settings", "set", "--clear-human-review-paths")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"merge_policy": map[string]any{"human_review_paths": []any{}}}, fake.lastUpdate(t))

	fake.set(func(s *lavaflowServer) {
		s.refuse, s.refuseMethod = http.StatusBadRequest, http.MethodPut
		s.refusal = map[string]any{"error": "allow_ci_before_approval is only allowed in auto mode"}
	})
	_, err = executeLavaflowCommand(t, New(deps), "settings", "set", "--allow-ci-before-approval", "true")
	require.EqualError(t, err, "failed to change LavaFlow's settings: HTTP 400: allow_ci_before_approval is only allowed in auto mode")
}

// humanMergeMode is the default merge mode's wire value, whichever the API's
// contract settles on.
const humanMergeMode = string(apiclient.LavaflowHumanMergeMode)

// settings set --help says what each merge mode does, in the approved copy,
// and what's true in every mode.
func TestSettingsSetHelpSaysWhatEachMergeModeDoes(t *testing.T) {
	_, deps := newLavaflowServer(t)

	out, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--help")
	require.NoError(t, err)
	assert.Contains(t, out, "  "+humanMergeMode+" (the default): A person approves every merge.\n")
	assert.Contains(t, out, "  risk_based: LavaFlow merges a fix on its own only when the policy rates it low risk, and holds the rest for a person.\n")
	assert.Contains(t, out, "  auto: LavaFlow merges every fix that passes review and required checks, unless a hold applies (held paths, rules it can't vouch for).\n")
	assert.Contains(t, out, "In every mode: Only a person's approval in Volcano releases a hold; "+
		"GitHub reviews don't. Reviewer concerns block merging.\n")
}
