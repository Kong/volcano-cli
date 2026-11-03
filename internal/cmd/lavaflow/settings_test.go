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
			"--custom-context, or --system-prompt"},
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
	assert.Contains(t, out, "also merges them:\n"+clilavaflow.MergeDisclosure+"\n--autonomy merge prints that and asks first; "+
		"--yes skips the question.")
	assert.Contains(t, out, "--human-review-path names a path whose changes always hold a fix for a")
	assert.NotContains(t, out, "approv")
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
		assert.Contains(t, out, "Autonomy: open fix pull requests and merge those that pass its review and your checks")
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

// The API refuses autonomy merge while merging is off for the project, or
// while the owner's GitHub account can't push to the repository, and the CLI
// says which.
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
			http.StatusForbidden, "the owner's GitHub connection needs reconnecting", "permission_missing",
			"LavaFlow can't merge for this project: the project owner's GitHub account can't push to its repository " +
				"(the owner's GitHub connection needs reconnecting). To connect GitHub, see " +
				"'volcano docs get platform/projects/git-deploy.md#1-connect-github'",
		},
	} {
		fake, deps := newLavaflowServer(t)
		fake.set(func(s *lavaflowServer) {
			s.refuse, s.refuseMethod = tc.status, http.MethodPut
			s.refusal = map[string]any{"error": tc.message, "code": tc.code}
		})

		_, err := executeLavaflowCommand(t, New(deps), "settings", "set", "--autonomy", "merge", "--yes")
		require.Error(t, err, name)
		assert.Equal(t, tc.want, err.Error(), name)
		assert.Equal(t, tc.status, api.Status(err), name)
	}
}
