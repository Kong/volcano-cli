package git

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

func TestReposListsEveryInstallation(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.installationsByConnection[gitConnectionID] = append(api.installationsByConnection[gitConnectionID],
		installation(otherInstall, "acme", "Organization", "selected"))
	fresh := repository("acme/new-site")
	fresh["is_empty"], fresh["private"] = true, false
	api.reposByInstallation[otherInstall] = []map[string]any{fresh}

	out, err := executeGitCommand(t, api.serve(), nil, "", "repos")
	require.NoError(t, err)

	assert.Contains(t, out, "GitHub account: octo")
	assert.Contains(t, out, "octo (user, all repositories)")
	assert.Contains(t, out, "  octo/storefront private, default branch main")
	assert.Contains(t, out, "acme (organization, selected repositories)")
	assert.Contains(t, out, "  acme/new-site public, empty")
	assert.Contains(t, out, "Connect one with volcano git connect https://github.com/<owner>/<repo>")
}

func TestReposPrintsJSON(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "", "repos", "--json")
	require.NoError(t, err)

	var listed []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &listed), out)
	require.Len(t, listed, 1)
	assert.Equal(t, "octo", listed[0]["github_account"])
	assert.Equal(t, "octo", listed[0]["account_login"])
	assert.Equal(t, "all", listed[0]["repository_selection"])
	repositories, ok := listed[0]["repositories"].([]any)
	require.True(t, ok)
	require.Len(t, repositories, 1)
	assert.Equal(t, "octo/storefront", repositories[0].(map[string]any)["full_name"])
}

// A list with an installation missing would read as "the App cannot see those
// repositories", so one failed lookup fails the list.
func TestReposFailsWhenAnInstallationCannotBeListed(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.repositoriesStatus[gitInstallation] = http.StatusBadGateway

	_, err := executeGitCommand(t, api.serve(), nil, "", "repos")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list repositories for octo")
}

func TestReposNeedsAGitHubConnection(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connections = nil

	_, err := executeGitCommand(t, api.serve(), nil, "", "repos")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GitHub account is connected")
}

func TestReposNeedsAnAccountToken(t *testing.T) {
	setGitCommandTestHome(t)
	setGitCommandTestToken(t, cliconfig.ProjectTokenPrefix+"token")
	api := newGitAPI(t)

	_, err := executeGitCommand(t, api.serve(), nil, "", "repos")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "account token")
	assert.Zero(t, api.accountRouteReads())
}

// Local mode has no GitHub App, and says so.
func TestReposExplainsAnAPIWithoutGitHub(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.providerStatus = http.StatusServiceUnavailable

	_, err := executeGitCommand(t, api.serve(), nil, "", "repos")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git provider integration is not configured on this API")
	assert.Contains(t, err.Error(), "If this is local mode, that is expected")
}

// Every route repos reads hangs off the account, so it works right after
// login, before a project is selected.
func TestReposNeedsNoSelectedProject(t *testing.T) {
	setGitCommandTestHome(t)
	require.NoError(t, (&cliconfig.Config{UserToken: "token"}).Save())
	api := newGitAPI(t)

	out, err := executeGitCommand(t, api.serve(), nil, "", "repos")
	require.NoError(t, err)
	assert.Contains(t, out, "octo/storefront")
}
