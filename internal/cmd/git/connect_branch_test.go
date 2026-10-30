package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// emptyRepository is a repository with no commits, which has no default branch.
func emptyRepository(fullName string) map[string]any {
	repo := repository(fullName)
	repo["default_branch"] = ""
	repo["is_empty"] = true
	return repo
}

// A repository with no commits has no default branch for the binding to
// follow, so the platform refuses to bind it without one. The CLI says so
// before asking, and names the flag that fixes it.
func TestConnectNeedsABranchForARepositoryWithNoCommits(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.reposByInstallation[gitInstallation] = []map[string]any{emptyRepository("octo/storefront")}

	_, err := executeGitCommand(t, api.serve(), nil, "",
		"connect", "https://github.com/octo/storefront", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "octo/storefront has no commits")
	assert.Contains(t, err.Error(), "volcano git connect https://github.com/octo/storefront --production-branch main")
	assert.Nil(t, api.sentConnectBody())
}

func TestConnectNamesTheBranchOfAnEmptyRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.reposByInstallation[gitInstallation] = []map[string]any{emptyRepository("octo/storefront")}

	_, err := executeGitCommand(t, api.serve(), nil, "",
		"connect", "https://github.com/octo/storefront", "--production-branch", "main", "--yes")
	require.NoError(t, err)
	assert.Equal(t, "main", api.sentConnectBody()["production_branch"])
}

func TestConnectRefusesABranchItCannotBind(t *testing.T) {
	for _, tc := range []struct {
		name      string
		connected string
		branch    string
		want      string
	}{
		{
			name: "same repository", connected: "octo/storefront", branch: "release",
			want: "octo/storefront is already connected, deploying from main",
		},
		{
			name: "another repository", connected: "octo/legacy", branch: "release",
			want: "a new repository is connected on its default branch, main, not release",
		},
		{name: "empty", branch: " ", want: "--production-branch is empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setGitCommandTestHome(t)
			api := newGitAPI(t)
			api.connected = tc.connected

			_, err := executeGitCommand(t, api.serve(), nil, "",
				"connect", "https://github.com/octo/storefront", "--production-branch", tc.branch, "--yes")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
			assert.Nil(t, api.sentConnectBody())
		})
	}
}

// Re-running connect with the branch already bound is a no-op, like any other
// re-run.
func TestConnectAcceptsTheBranchAlreadyBound(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	out, err := executeGitCommand(t, api.serve(), nil, "",
		"connect", "https://github.com/octo/storefront", "--production-branch", "main", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "already connected")
	assert.Nil(t, api.sentConnectBody())
}

// A repository with no commits has no default branch, so the platform takes the
// named branch even when the connect replaces another repository: there is no
// echo of the old repository's branch to guard against.
func TestConnectReplacesWithAnEmptyRepositoryOnTheNamedBranch(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/legacy"
	api.reposByInstallation[gitInstallation] = []map[string]any{emptyRepository("octo/storefront")}

	_, err := executeGitCommand(t, api.serve(), nil, "",
		"connect", "https://github.com/octo/storefront", "--production-branch", "main", "--yes")
	require.NoError(t, err)
	assert.Equal(t, "main", api.sentConnectBody()["production_branch"])
}

// The hint repeats the command without the URL as typed, which can carry a
// credential, and keeps the root directory asked for.
func TestConnectHintForAnEmptyRepositoryRedactsTheURL(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.reposByInstallation[gitInstallation] = []map[string]any{emptyRepository("octo/storefront")}

	_, err := executeGitCommand(t, api.serve(), nil, "",
		"connect", "https://x-access-token:secret-token@github.com/octo/storefront.git", "--root-directory", "apps/web", "--yes")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-token")
	assert.Contains(t, err.Error(),
		"volcano git connect https://github.com/octo/storefront --root-directory apps/web --production-branch main")
}
