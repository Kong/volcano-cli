package git

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestExportPushesTheConfiguredBranch(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "export", "--yes")
	require.NoError(t, err)

	assert.Equal(t, map[string]any{"production_branch": "main"}, api.sentExportBody())
	assert.Contains(t, out, "Exported 12 files to octo/storefront")
	assert.Contains(t, out, "Branch: main")
	assert.Contains(t, out, "Commit: 6f2b1c9a4e8d3f7b0a5c2e9d1f4b8a6c3e7d0b25")
	assert.Contains(t, out, "Follow it with volcano git status")
}

// A pinned branch is the one exported, and --branch confirms it.
func TestExportConfirmsAPinnedBranch(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedBranch, api.branchPinned = "release", true

	_, err := executeGitCommand(t, api.serve(), nil, "", "export", "--branch", "release", "--yes")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"production_branch": "release"}, api.sentExportBody())
}

// --branch says which branch the caller expects. Exporting to another one would
// create a branch the caller never agreed to, so nothing is sent.
func TestExportRefusesABranchThatIsNotTheProductionBranch(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommand(t, api.serve(), nil, "", "export", "--branch", "release", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the production branch of octo/storefront is main, not release")
	assert.Contains(t, err.Error(), "volcano git production-branch release")
	exports, _ := api.exportCalls()
	assert.Zero(t, exports)
}

func TestExportRejectsAnEmptyBranch(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommand(t, api.serve(), nil, "", "export", "--branch", " ", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--branch is empty")
}

func TestExportListsWhatItCouldNotCarry(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.exportResult = map[string]any{
		"repo_full_name": "octo/storefront",
		"branch":         "main",
		"commit_sha":     "6f2b1c9a",
		"file_count":     1,
		"skipped":        []any{map[string]any{"kind": "function", "name": "draft", "reason": "no successful deployment"}},
		"omitted": []any{
			map[string]any{"kind": "migrations", "resource": "", "path": "volcano/migrations"},
			map[string]any{"kind": "ignored_secret", "resource": "web", "path": "frontends/web/.env"},
		},
	}

	out, err := executeGitCommand(t, api.serve(), nil, "", "export", "--yes")
	require.NoError(t, err)

	assert.Contains(t, out, "Exported 1 file to octo/storefront")
	assert.Contains(t, out, "  function draft: no successful deployment")
	assert.Contains(t, out, "  migrations volcano/migrations\n")
	assert.Contains(t, out, "  ignored_secret frontends/web/.env (web)")
}

// --json prints only the platform's result, so a script can parse stdout.
func TestExportPrintsJSON(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "", "export", "--yes", "--json")
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result), out)
	assert.Equal(t, "octo/storefront", result["repo_full_name"])
	assert.Equal(t, "main", result["branch"])
	assert.InDelta(t, 12, result["file_count"], 0)
}

// Export runs once and cannot be undone, so it asks first and names where the
// source goes.
func TestExportAsksFirst(t *testing.T) {
	for _, tc := range []struct {
		answer  string
		exports int
	}{
		{answer: "n\n", exports: 0},
		{answer: "y\n", exports: 1},
	} {
		t.Run(tc.answer[:1], func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.connected = "octo/storefront"

			out, err := executeGitCommand(t, api.serve(), nil, tc.answer, "export")
			require.NoError(t, err)
			assert.Contains(t, out, "to octo/storefront as the first commit on main")
			// The prompt states what the user cannot take back.
			assert.Contains(t, out, "Anyone who can read octo/storefront can read that source.")
			assert.Contains(t, out, "this cannot be undone")
			assert.Contains(t, out, "can never be disconnected from it or moved to another repository")
			assert.Contains(t, out, "Export to octo/storefront?")
			exports, _ := api.exportCalls()
			assert.Equal(t, tc.exports, exports)
		})
	}
}

// Without a terminal to answer the prompt, export refuses unless --yes says to
// go ahead, and still shows what it would have done.
func TestExportRefusesWhenStdinCannotAnswer(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	closed, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { _ = closed.Close() })

	cmd := New(cliruntime.Deps{HTTPClient: api.serve().Client(), APIBaseURL: api.serve().URL})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(closed)
	cmd.SetArgs([]string{"export"})

	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdin is not a terminal")
	assert.Contains(t, err.Error(), "--yes")
	assert.Contains(t, out.String(), "this cannot be undone")
	exports, _ := api.exportCalls()
	assert.Zero(t, exports)
}

// An export the platform is certain to refuse stops before the prompt, and
// says what to do instead.
func TestExportStopsWhenTheSourceWasAlreadyExported(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       string
		exportedAt string
		want       string
	}{
		{name: "handed over", mode: "git", exportedAt: "2026-08-25T09:14:04Z", want: "already its source of truth"},
		{name: "deploying", mode: "git_pending", exportedAt: "2026-08-25T09:14:04Z", want: "cancel it with volcano git export cancel"},
		{name: "canceled after its commit", mode: "platform", exportedAt: "2026-08-25T09:14:04Z", want: "only once"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.connected = "octo/storefront"
			api.sourceMode, api.sourceExportedAt = tc.mode, tc.exportedAt

			_, err := executeGitCommand(t, api.serve(), nil, "", "export", "--yes")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			exports, _ := api.exportCalls()
			assert.Zero(t, exports)
		})
	}
}

// Retrying is how an export whose push GitHub did not confirm is resolved, so
// git_exporting does not stop it.
func TestExportRetriesAnUnconfirmedPush(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.sourceMode = "git_exporting"

	_, err := executeGitCommand(t, api.serve(), nil, "", "export", "--yes")
	require.NoError(t, err)
	exports, _ := api.exportCalls()
	assert.Equal(t, 1, exports)
}

func TestExportNeedsAConnectedRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "export", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this project is not connected to a repository")
	assert.Contains(t, err.Error(), "Connect one with volcano git connect")
	exports, _ := api.exportCalls()
	assert.Zero(t, exports)
}

// The platform's refusal names what is wrong, such as a repository that is not
// empty, so it is reported as given.
func TestExportReportsThePlatformsRefusal(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.exportStatus = http.StatusConflict
	api.exportMessage = "The connected repository is not empty. Export requires a new repository with no commits or branches."

	_, err := executeGitCommand(t, api.serve(), nil, "", "export", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to export the project's source")
	assert.Contains(t, err.Error(), "The connected repository is not empty.")
}

// Local mode serves functions from the host, so it has no stored source. Both
// commands say so before reading the binding or asking anything.
func TestExportExplainsAnAPIWithoutSourceExport(t *testing.T) {
	for _, args := range [][]string{
		{"export", "--yes"},
		{"export", "cancel", "--yes"},
	} {
		t.Run(args[len(args)-2], func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.connected = "octo/storefront"
			api.sourceStatus = http.StatusNotImplemented

			_, err := executeGitCommand(t, api.serve(), nil, "", args...)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "source export is not available on this API")
			assert.Contains(t, err.Error(), "If this is local mode, that is expected")
			exports, cancels := api.exportCalls()
			assert.Zero(t, exports+cancels)
		})
	}
}

func TestExportCancelResumesDirectDeploys(t *testing.T) {
	for _, tc := range []struct {
		name     string
		consumes bool
		want     string
	}{
		{name: "before its commit", want: "The repository was not changed."},
		{name: "after its commit", consumes: true, want: "Export cannot run again for this project."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.connected = "octo/storefront"
			api.sourceMode = "git_exporting"
			api.cancelConsumes = tc.consumes

			out, err := executeGitCommand(t, api.serve(), nil, "", "export", "cancel", "--yes")
			require.NoError(t, err)

			assert.Contains(t, out, "Canceled the source export. Direct deploys work again.")
			assert.Contains(t, out, tc.want)
			_, cancels := api.exportCalls()
			assert.Equal(t, 1, cancels)
		})
	}
}

// --json reports the state after the cancel, which says whether export can run
// again.
func TestExportCancelPrintsJSON(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.sourceMode = "git_pending"
	api.sourceExportedAt = "2026-08-25T09:14:04Z"

	out, err := executeGitCommand(t, api.serve(), nil, "", "export", "cancel", "--yes", "--json")
	require.NoError(t, err)

	var state map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &state), out)
	assert.Equal(t, "platform", state["mode"])
	assert.Equal(t, "2026-08-25T09:14:04Z", state["exported_at"])
}

func TestExportCancelNeedsAnIncompleteExport(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want string
	}{
		{mode: "platform", want: "no incomplete source export to cancel"},
		{mode: "git", want: "its repository is the source of truth"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.sourceMode = tc.mode

			_, err := executeGitCommand(t, api.serve(), nil, "", "export", "cancel", "--yes")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			_, cancels := api.exportCalls()
			assert.Zero(t, cancels)
		})
	}
}

func TestExportCancelAsksFirst(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.sourceMode = "git_pending"

	out, err := executeGitCommand(t, api.serve(), nil, "n\n", "export", "cancel")
	require.NoError(t, err)
	assert.Contains(t, out, "Cancel the source export?")
	_, cancels := api.exportCalls()
	assert.Zero(t, cancels)
}

// Once the first commit exists, cancelling gives up the one export the project
// gets, and the prompt says so.
func TestExportCancelSaysWhenExportCannotRunAgain(t *testing.T) {
	for _, tc := range []struct {
		name, mode, exportedAt string
		final                  bool
	}{
		{name: "before its commit", mode: "git_exporting"},
		{name: "after its commit", mode: "git_exporting", exportedAt: "2026-08-25T09:14:04Z", final: true},
		{name: "pending its deploy", mode: "git_pending", final: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.sourceMode, api.sourceExportedAt = tc.mode, tc.exportedAt

			out, err := executeGitCommand(t, api.serve(), nil, "n\n", "export", "cancel")
			require.NoError(t, err)
			if tc.final {
				assert.Contains(t, out, "Export cannot run again for this project.")
			} else {
				assert.NotContains(t, out, "cannot run again")
			}
		})
	}
}

func TestStatusReportsTheSourceOfTruth(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		status int
		want   []string
		absent []string
	}{
		{mode: "platform", want: []string{"Source of truth: Volcano"}, absent: []string{"git export cancel"}},
		{mode: "git_pending", want: []string{"Source of truth: moving to the repository", "volcano git export cancel"}},
		{mode: "git", want: []string{"Source of truth: the repository"}, absent: []string{"git export cancel"}},
		{mode: "unavailable", status: http.StatusNotImplemented, absent: []string{"Source of truth"}},
		{mode: "unreadable", status: http.StatusInternalServerError, want: []string{"Source of truth: unknown"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.connected = "octo/storefront"
			api.sourceMode, api.sourceStatus = tc.mode, tc.status

			out, err := executeGitCommand(t, api.serve(), nil, "", "status")
			require.NoError(t, err)
			for _, want := range tc.want {
				assert.Contains(t, out, want)
			}
			for _, absent := range tc.absent {
				assert.NotContains(t, out, absent)
			}
		})
	}
}

// The export names only the branch, so a repository reconnected while the
// prompt was open would receive the source unseen. The binding is read again
// first, and nothing is exported when it moved.
func TestExportRefusesWhenTheRepositoryChangedWhileAsking(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.connectedAfterRead = "octo/elsewhere"

	_, err := executeGitCommand(t, api.serve(), nil, "", "export", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "changed while this command was running")
	assert.Contains(t, err.Error(), "octo/elsewhere")
	exports, _ := api.exportCalls()
	assert.Zero(t, exports)
}
