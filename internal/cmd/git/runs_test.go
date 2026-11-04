package git

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliapi "github.com/Kong/volcano-cli/internal/api"
	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

const (
	failedRunID    = "6f1c2b9a-4e8d-4f7b-8a5c-2e9d1f4b8a6c"
	succeededRunID = "7a2d3c0b-5f9e-4a8c-9b6d-3f0e2a5c9b7d"
)

// failedRun is a push whose migrations failed, in the API's own words.
func failedRun() map[string]any {
	run := gitRun(failedRunID, "failed")
	run["apply_migrations"] = true
	run["error"] = map[string]any{
		"code":    "migration_failed",
		"message": "migration 002_add_orders.sql failed on database app: relation \"orders\" already exists",
	}
	run["migration_runs"] = []any{map[string]any{
		"id":       "8b3e4d1c-6a0f-4b9d-8c7e-4a1f3b6d0c8e",
		"database": map[string]any{"type": "database", "id": "9c4f5e2d-7b1a-4c0e-9d8f-5b2a4c7e1d9f", "name": "app"},
	}}
	run["started_at"] = "2026-11-04T10:00:05Z"
	run["completed_at"] = "2026-11-04T10:01:00Z"
	return run
}

// configuredRun applied the push's frontends section, holding a route for a
// frontend another repository deploys.
func configuredRun() map[string]any {
	run := gitRun(succeededRunID, "succeeded")
	run["config"] = map[string]any{
		"status": "applied",
		"sections": []any{map[string]any{
			"section": "frontends", "created": 1, "updated": 2, "unchanged": 3, "failed": 0,
			"held": map[string]any{"frontend_not_deployed_by_repository": 1},
		}},
	}
	return run
}

func TestRunsListShowsEachPush(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"
	api.runs = []map[string]any{failedRun(), gitRun(succeededRunID, "succeeded")}

	out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "list")
	require.NoError(t, err)

	assert.Contains(t, out, failedRunID)
	assert.Contains(t, out, succeededRunID)
	assert.Contains(t, out, "failed")
	assert.Contains(t, out, "migration_failed")
	assert.Contains(t, out, "6f2b1c9 ")
	assert.Contains(t, out, "Showing 2 run(s)")
	assert.Contains(t, out, "Show one with volcano git runs get <run-id>")
	assert.NotContains(t, out, "Config", "no run on the page reports its config")
	assert.Equal(t, "20", api.sentRunsQuery().Get("limit"))
	assert.Zero(t, api.accountRouteReads(), "a page with runs needs nothing else")
}

// The filters reach the API, and the next page keeps them, since the cursor
// is bound to them.
func TestRunsListFiltersAndPages(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.runs = []map[string]any{failedRun(), gitRun(succeededRunID, "succeeded")}
	api.runsNextCursor = "cursor-2"

	out, err := executeGitCommand(t, api.serve(), nil, "",
		"runs", "list", "--status", "failed", "--branch", "main", "--limit", "1")
	require.NoError(t, err)

	query := api.sentRunsQuery()
	assert.Equal(t, "failed", query.Get("status"))
	assert.Equal(t, "main", query.Get("branch"))
	assert.Equal(t, "1", query.Get("limit"))
	assert.NotContains(t, out, succeededRunID)
	assert.Contains(t, out, "Next page: volcano git runs list --limit 1 --cursor cursor-2 --status failed --branch main")
}

// A run that reports what applying its volcano-config.yaml sections did gets
// a Config column; a run that does not shows a dash there.
func TestRunsListShowsTheConfigResultWhenARunHasOne(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.runs = []map[string]any{configuredRun(), failedRun()}

	out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "list")
	require.NoError(t, err)

	assert.Contains(t, out, "Config")
	assert.Contains(t, out, "applied")
}

func TestRunsListWithNoRuns(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No Git deployment runs. Each push to the production branch records one.")
}

// --json prints the API's page, with each run's config result passed on whole.
func TestRunsListJSON(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.runs = []map[string]any{configuredRun(), failedRun()}

	out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "list", "--json")
	require.NoError(t, err)

	var page struct {
		Data    []map[string]any `json:"data"`
		HasMore bool             `json:"has_more"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &page), out)
	require.Len(t, page.Data, 2)
	assert.Equal(t, jsonRoundTrip(t, configuredRun()["config"]), page.Data[0]["config"])
	assert.NotContains(t, page.Data[1], "config", "a run without a config result has no config key")
	assert.Equal(t, "migration_failed", page.Data[1]["error"].(map[string]any)["code"])
}

// Runs come from pushes the GitHub App delivers, so where the API has none,
// as in local mode, no run is ever recorded, and the commands say why rather
// than report an empty history.
func TestRunsWhereNoRepositoryCanBeConnected(t *testing.T) {
	for _, args := range [][]string{{"runs", "list"}, {"runs", "get", failedRunID}} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.providerStatus = http.StatusServiceUnavailable

		_, err := executeGitCommand(t, api.serve(), nil, "", args...)
		require.Error(t, err, "%q", args)
		assert.Contains(t, err.Error(), "git provider integration is not configured on this API", "%q", args)
		assert.Contains(t, err.Error(), "If this is local mode, that is expected", "%q", args)
	}
}

// Reading runs is what CI and agents do after a push, so a project access
// token may, and an empty history cannot be checked against the account's
// GitHub connection with one.
func TestRunsWorkWithAProjectAccessToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No Git deployment runs.")
	assert.Zero(t, api.accountRouteReads())

	api.runs = []map[string]any{failedRun()}
	out, err = executeGitCommand(t, api.serve(), nil, "", "runs", "get", failedRunID)
	require.NoError(t, err)
	assert.Contains(t, out, "Run: "+failedRunID)
}

func TestRunsGetShowsWhyAPushFailed(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.runs = []map[string]any{failedRun()}

	out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "get", failedRunID)
	require.NoError(t, err)

	assert.Contains(t, out, "Run: "+failedRunID)
	assert.Contains(t, out, "Status: failed")
	assert.Contains(t, out, "Branch: main")
	assert.Contains(t, out, "Commit: 6f2b1c9a4e8d3f7b0a5c2e9d1f4b8a6c3e7d0b25")
	assert.Contains(t, out, "Migrations: applied before deploying")
	assert.Contains(t, out, "Migration run: 8b3e4d1c-6a0f-4b9d-8c7e-4a1f3b6d0c8e on database app")
	assert.Contains(t, out, `migration_failed: migration 002_add_orders.sql failed on database app: relation "orders" already exists`)
	assert.NotContains(t, out, "Config:", "the run reports no config result")
}

// What applying the push's volcano-config.yaml sections did reads per
// section, with what the push left for config deploy and why.
func TestRunsGetShowsTheConfigResult(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.runs = []map[string]any{configuredRun()}

	out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "get", succeededRunID)
	require.NoError(t, err)

	assert.Contains(t, out, "Config: applied\n")
	assert.Contains(t, out, "  frontends: 1 created, 2 updated, 3 unchanged, 0 failed\n")
	assert.Contains(t, out, "    frontend_not_deployed_by_repository: 1 left for config deploy\n")
}

// A config result that fails names its code; one in a shape this CLI does not
// know still says there is one, and --json shows it whole.
func TestRunsGetConfigResultVariants(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config any
		want   string
	}{
		{name: "failed", config: map[string]any{"status": "failed", "code": "config_invalid"}, want: "Config: failed (config_invalid)\n"},
		{name: "unknown shape", config: []any{"applied"}, want: "Config: reported in a form this CLI can't read; see --json\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			run := gitRun(succeededRunID, "failed")
			run["config"] = tc.config
			api.runs = []map[string]any{run}

			out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "get", succeededRunID)
			require.NoError(t, err)
			assert.Contains(t, out, tc.want)

			out, err = executeGitCommand(t, api.serve(), nil, "", "runs", "get", succeededRunID, "--json")
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, json.Unmarshal([]byte(out), &got), out)
			assert.Equal(t, jsonRoundTrip(t, tc.config), got["config"])
		})
	}
}

func TestRunsGetJSON(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.runs = []map[string]any{failedRun()}

	out, err := executeGitCommand(t, api.serve(), nil, "", "runs", "get", failedRunID, "--json")
	require.NoError(t, err)

	var run map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &run), out)
	assert.Equal(t, failedRunID, run["id"])
	assert.Equal(t, "failed", run["status"])
	assert.Equal(t, true, run["apply_migrations"])
	assert.NotContains(t, run, "config")
}

// A run the project does not have is not found, with that exit code.
func TestRunsGetARunThatDoesNotExist(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "runs", "get", failedRunID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "this project has no Git deployment run with that ID: "+failedRunID)
	assert.Equal(t, http.StatusNotFound, cliapi.Status(err))
}

// A run ID is a UUID, so anything else is refused before a request the
// client cannot make.
func TestRunsGetRefusesWhatIsNotARunID(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "runs", "get", "latest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"latest" is not a run ID: a run ID is a UUID`)
	assert.Nil(t, api.sentRunsQuery())
}

// An API older than Git deployment runs has no route for them, which says so
// rather than reading as an empty history or a missing run.
func TestRunsOnAnAPIWithoutThem(t *testing.T) {
	for _, args := range [][]string{{"runs", "list"}, {"runs", "get", failedRunID}} {
		setGitCommandTestHome(t)
		api := newGitAPI(t)
		api.runsUnsupported = true

		_, err := executeGitCommand(t, api.serve(), nil, "", args...)
		require.Error(t, err, "%q", args)
		assert.Contains(t, err.Error(), "this Volcano API doesn't record Git deployment runs yet", "%q", args)
	}
}

// jsonRoundTrip is value as it reads back from JSON, so a fixture compares
// with what a command printed.
func jsonRoundTrip(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	var decoded any
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return decoded
}
