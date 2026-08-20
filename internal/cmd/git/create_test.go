package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The happy path, end to end: a repository is created, the project is bound to
// it, and this checkout is pushed — the last part being the whole reason for
// doing this from the CLI rather than from the API.
func TestGitCreateCreatesConnectsAndPushes(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{output: "Everything up-to-date\n"}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"create", "storefront", "--yes")
	require.NoError(t, err)

	body := api.sentCreateBody()
	assert.Equal(t, "storefront", body["name"])
	// Private by default: the next step is pushing the project's source into it.
	assert.Equal(t, true, body["private"])
	assert.NotContains(t, body, "owner", "an omitted owner must stay omitted; the platform resolves it")

	assert.Contains(t, out, "Created octo/storefront")
	assert.Contains(t, out, "Pushed main to octo/storefront")
	assert.Contains(t, out, "A push to main deploys: functions")
	assert.Contains(t, runner.ran(), "git remote add -- origin https://github.com/octo/storefront.git")
	assert.Equal(t, []string{"git push --set-upstream -- origin main"}, terminal.ran())
	// git's own progress reaches the user rather than being captured and dropped.
	assert.Contains(t, out, "Everything up-to-date")
}

// The branch this checkout is on is sent as the production branch. A repository
// created empty has no default branch, so the platform would otherwise predict
// one from the account's setting, and a wrong prediction leaves the project
// connected and never deploying.
func TestGitCreateSendsTheBranchItIsAboutToPush(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("trunk", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"create", "storefront", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "trunk", api.sentCreateBody()["production_branch"])
	assert.Contains(t, out, "Production branch")
	assert.Contains(t, out, "trunk")
	assert.Equal(t, []string{"git push --set-upstream -- origin trunk"}, terminal.ran())
}

func TestGitCreateNamesTheRepositoryAfterTheDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	working := filepath.Join(t.TempDir(), "storefront")
	require.NoError(t, os.MkdirAll(working, 0o755))
	t.Chdir(working)

	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "", "create", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "storefront", api.sentCreateBody()["name"])
	assert.Contains(t, out, "Created octo/storefront")
}

// GitHub silently replaces the characters it does not allow, so a directory
// named "my app" would become "my-app" — a repository under a name nobody chose,
// which this command would then report as the name that was asked for.
func TestGitCreateRefusesANameGitHubWouldRewrite(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"create", "my app", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "which GitHub does not allow")
	assert.Zero(t, api.createAttempts(), "nothing may be created for a name that cannot be used")
}

func TestGitCreateRefusesAnEmptyName(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "", "create", "   ", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the repository name is empty")
	assert.Zero(t, api.createAttempts())
}

// A project holds one repository, so creating a second is refused by the
// platform — but only after the repository exists, in one of the two races it
// guards. The cheap read here is what keeps the ordinary mistake from costing an
// orphaned repository.
func TestGitCreateRefusesAProjectThatIsAlreadyConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"create", "another", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already connected to octo/storefront")
	assert.Contains(t, err.Error(), "volcano git disconnect")
	assert.Zero(t, api.createAttempts())
}

func TestGitCreateRefusesACheckoutWithNothingToPush(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	// An unborn HEAD: git init has run, nothing is committed.
	delete(runner.outputs, "git rev-parse --quiet --verify HEAD")

	_, err := executeGitCommandWith(t, api.serve(), runner, nil, "", "create", "storefront", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no commits")
	assert.Contains(t, err.Error(), "--no-push")
	assert.Zero(t, api.createAttempts(), "a repository that cannot be pushed to must not be created")
}

func TestGitCreateRefusesADirectoryThatIsNotARepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	// Nothing registered, so every git read fails the way it does outside a
	// repository.
	_, err := executeGitCommandWith(t, api.serve(), &gitRunner{outputs: map[string]string{}}, nil, "",
		"create", "storefront", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a Git work tree")
	assert.Contains(t, err.Error(), "--no-push")
	assert.Zero(t, api.createAttempts())
}

// Taking over a remote name this checkout already uses would silently redirect
// where it pushes, so it is refused — before anything is created, because git
// would only refuse it afterwards.
func TestGitCreateRefusesARemoteNameAlreadyInUse(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", originRemoteOutput), nil, "",
		"create", "storefront", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `already has a remote named "origin"`)
	assert.Contains(t, err.Error(), "--remote")
	assert.Zero(t, api.createAttempts())
}

func TestGitCreateUsesAnotherRemoteName(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", originRemoteOutput)
	runner.allow("git remote add -- volcano https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"create", "storefront", "--remote", "volcano", "--yes")
	require.NoError(t, err)

	assert.Equal(t, []string{"git push --set-upstream -- volcano main"}, terminal.ran())
	assert.Contains(t, out, "Remote")
	assert.Contains(t, out, "volcano")
}

// --branch names what deploys, so it is also what gets pushed. A branch that is
// not in this checkout cannot be pushed, and pushing the current one instead
// would deploy nothing.
func TestGitCreateRefusesABranchThatIsNotInTheCheckout(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"create", "storefront", "--branch", "release", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--branch release does not exist in this checkout")
	assert.Contains(t, err.Error(), "main")
	assert.Zero(t, api.createAttempts())
}

func TestGitCreatePushesANamedBranchThatExists(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.outputs["git rev-parse --quiet --verify refs/heads/release"] = "d34db33f\n"
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{}

	_, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"create", "storefront", "--branch", "release", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "release", api.sentCreateBody()["production_branch"])
	assert.Equal(t, []string{"git push --set-upstream -- origin release"}, terminal.ran())
}

func TestGitCreateRefusesABranchGitWouldReadAsAnOption(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
		"create", "storefront", "--branch", "-delete", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git reads it as an option")
	assert.Zero(t, api.createAttempts())
}

// --no-push means the checkout is not touched at all, so the report has to hand
// back both steps the user is left with — not just the push.
func TestGitCreateWithNoPushLeavesTheCheckoutAlone(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	terminal := &gitTerminalRunner{}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"create", "storefront", "--no-push", "--yes")
	require.NoError(t, err)

	assert.Empty(t, runner.ran(), "--no-push must not read or write the checkout")
	assert.Empty(t, terminal.ran())
	assert.Contains(t, out, "git remote add origin https://github.com/octo/storefront.git")
	assert.Contains(t, out, "git push --set-upstream origin main")
	// With nothing local resolved, the branch printed is the one the platform
	// bound — printing anything else would print a command that deploys nothing.
	assert.NotContains(t, api.sentCreateBody(), "production_branch")
}

// The failure that produces no error anywhere: the push succeeds, the code
// lands, and nothing ever deploys. So it is not attempted, and the access to
// grant is reported instead.
func TestGitCreateSkipsThePushWhenTheAppCannotSeeTheRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.createAppInstalled = false
	api.createInstallURL = "https://github.com/apps/volcano/installations/new"
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"create", "storefront", "--yes")
	require.NoError(t, err, "the repository and the binding both exist; this is not a failure")

	assert.Empty(t, terminal.ran(), "a push that deploys nothing must not be presented as progress")
	assert.Contains(t, out, "could not be confirmed to have access")
	assert.Contains(t, out, "https://github.com/apps/volcano/installations/new")
	// The remote is still recorded: it is what the user pushes with once access
	// is granted, and re-running create is not an option.
	assert.Contains(t, runner.ran(), "git remote add -- origin https://github.com/octo/storefront.git")
	assert.Contains(t, out, "git push --set-upstream origin main")
	assert.NotContains(t, out, "git remote add origin")
}

// The repository exists and the project is bound by the time the push runs, so a
// push failure may not read as "nothing happened".
func TestGitCreateReportsTheRepositoryWhenThePushFails(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{err: errors.New("exit status 128")}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"create", "storefront", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "octo/storefront was created and connected")
	assert.Contains(t, err.Error(), "the local step did not finish")
	// And the report still describes what exists, so the user is not left
	// guessing whether the repository is there.
	assert.Contains(t, out, "Created octo/storefront")
	assert.NotContains(t, out, "Pushed")
}

func TestGitCreatePublicRepositorySendsPrivateFalse(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	_, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"create", "storefront", "--public", "--yes")
	require.NoError(t, err)
	assert.Equal(t, false, api.sentCreateBody()["private"])
}

func TestGitCreateRefusesPrivateAndPublicTogether(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
		"create", "storefront", "--private", "--public", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--private and --public cannot be combined")
	assert.Zero(t, api.createAttempts())
}

func TestGitCreateSendsAnOwnerTheAppIsInstalledOn(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.installationsByConnection[gitConnectionID] = append(
		api.installationsByConnection[gitConnectionID], installation(otherInstall, "acme", "Organization", "all"))
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/acme/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"create", "storefront", "--owner", "acme", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "acme", api.sentCreateBody()["owner"])
	assert.Contains(t, out, "Created acme/storefront")
}

// The platform refuses this too, with a 404 that cannot say which accounts would
// have worked. This can, because it has the installation list in hand.
func TestGitCreateRefusesAnOwnerTheAppIsNotInstalledOn(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"create", "storefront", "--owner", "acme", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed on that account: acme")
	assert.Contains(t, err.Error(), "It is installed on: octo")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
	assert.Zero(t, api.createAttempts())
}

// Retrying under a new name is the wrong reflex here: the repository may already
// be on GitHub, and a retry leaves the user owning two.
func TestGitCreateWarnsThatARepositoryMayExistOnConflict(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.createStatus = 409
	api.createErrorMessage = "octo/storefront was created but could not be connected"
	runner := checkoutRunner("main", "")

	_, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"create", "storefront", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may have been created on GitHub")
	assert.Contains(t, err.Error(), "octo/storefront was created but could not be connected")
	assert.Contains(t, err.Error(), "volcano git connect")
	assert.NotContains(t, runner.ran(), "git remote add -- origin https://github.com/octo/storefront.git",
		"no remote may be recorded for a repository that may not be bound")
}

func TestGitCreateRefusesAnAbsoluteRootDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
		"create", "storefront", "--root-directory", "/etc", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an absolute path")
	assert.Zero(t, api.createAttempts())
}

func TestGitCreateSendsTheRootDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"create", "storefront", "--root-directory", "apps/api", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "apps/api", api.sentCreateBody()["root_directory"])
	assert.Contains(t, out, "apps/api")
}

func TestGitCreateRecordsAnSSHRemoteWhenAsked(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin git@github.com:octo/storefront.git")

	_, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"create", "storefront", "--ssh", "--yes")
	require.NoError(t, err)
	assert.Contains(t, runner.ran(), "git remote add -- origin git@github.com:octo/storefront.git")
}

// Declining creates nothing and exits 0, as everywhere else in the CLI. The
// prompt matters more here than for any other git subcommand: it is the only one
// whose effect Volcano cannot undo.
func TestGitCreateDeclinedCreatesNothing(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "n\n",
		"create", "storefront")
	require.NoError(t, err)

	assert.Zero(t, api.createAttempts())
	assert.NotContains(t, runner.ran(), "git remote add -- origin https://github.com/octo/storefront.git")
	assert.Contains(t, out, "cannot be undone")
}
