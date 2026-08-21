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
func TestGitExportCreatesConnectsAndPushes(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{output: "Everything up-to-date\n"}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
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
func TestGitExportSendsTheBranchItIsAboutToPush(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("trunk", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--repo", "storefront", "--branch", "trunk", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "trunk", api.sentCreateBody()["production_branch"])
	assert.Contains(t, out, "Production branch")
	assert.Contains(t, out, "trunk")
	assert.Equal(t, []string{"git push --set-upstream -- origin trunk"}, terminal.ran())
}

func TestGitExportNamesTheRepositoryAfterTheDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	working := filepath.Join(t.TempDir(), "storefront")
	require.NoError(t, os.MkdirAll(working, 0o755))
	t.Chdir(working)

	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "", "export", "--repo", "storefront", "--branch", "main", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "storefront", api.sentCreateBody()["name"])
	assert.Contains(t, out, "Created octo/storefront")
}

// GitHub silently replaces the characters it does not allow, so a directory
// named "my app" would become "my-app" — a repository under a name nobody chose,
// which this command would then report as the name that was asked for.
func TestGitExportRefusesANameGitHubWouldRewrite(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"export", "--repo", "my app", "--branch", "main", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "which GitHub does not allow")
	assert.Zero(t, api.createAttempts(), "nothing may be created for a name that cannot be used")
}

func TestGitExportRefusesAnEmptyName(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "", "export", "--repo", "   ", "--branch", "main", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no repository")
	assert.Zero(t, api.createAttempts())
}

// A project holds one repository, so creating a second is refused by the
// platform — but only after the repository exists, in one of the two races it
// guards. The cheap read here is what keeps the ordinary mistake from costing an
// orphaned repository.
func TestGitExportRefusesAProjectThatIsAlreadyConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connected = "octo/storefront"

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"export", "--repo", "another", "--branch", "main", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already connected to octo/storefront")
	// Both ways out are dashboard flows now that the CLI only creates.
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
	assert.Zero(t, api.createAttempts())
}

func TestGitExportRefusesADirectoryThatIsNotARepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	// Exit 128 is what git gives outside a repository, and it is not the exit 1
	// that means "no such branch" — the two must not be reported alike.
	_, err := executeGitCommandWith(t, api.serve(), &gitRunner{outputs: map[string]string{}, failConfig: true}, nil, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not read this directory's Git repository")
	assert.Contains(t, err.Error(), "--no-push")
	assert.Zero(t, api.createAttempts())
}

// Taking over a remote name this checkout already uses would silently redirect
// where it pushes, so it is refused — before anything is created, because git
// would only refuse it afterwards.
func TestGitExportRefusesARemoteNameAlreadyInUse(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", originRemoteOutput), nil, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `already has a remote named "origin"`)
	assert.Contains(t, err.Error(), "--remote")
	assert.Zero(t, api.createAttempts())
}

func TestGitExportUsesAnotherRemoteName(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", originRemoteOutput)
	runner.allowRemoteName("volcano")
	runner.allow("git remote add -- volcano https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--branch", "main", "--repo", "storefront", "--remote", "volcano", "--yes")
	require.NoError(t, err)

	assert.Equal(t, []string{"git push --set-upstream -- volcano main"}, terminal.ran())
	assert.Contains(t, out, "Remote")
	assert.Contains(t, out, "volcano")
}

// --branch names what deploys, so it is also what gets pushed. A branch that is
// not in this checkout cannot be pushed, and pushing the current one instead
// would deploy nothing.
func TestGitExportRefusesABranchThatIsNotInTheCheckout(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"export", "--repo", "storefront", "--branch", "release", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "branch release does not exist in this checkout")
	assert.Contains(t, err.Error(), "--no-push")
	assert.Zero(t, api.createAttempts())
}

func TestGitExportPushesANamedBranchThatExists(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.outputs["git rev-parse --quiet --verify refs/heads/release"] = "d34db33f\n"
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{}

	_, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--repo", "storefront", "--branch", "release", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "release", api.sentCreateBody()["production_branch"])
	assert.Equal(t, []string{"git push --set-upstream -- origin release"}, terminal.ran())
}

func TestGitExportRefusesABranchGitWouldReadAsAnOption(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
		"export", "--repo", "storefront", "--branch", "-delete", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git reads it as an option")
	assert.Zero(t, api.createAttempts())
}

// --no-push means the checkout is not touched at all, so the report has to hand
// back both steps the user is left with — not just the push.
func TestGitExportWithNoPushLeavesTheCheckoutAlone(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	terminal := &gitTerminalRunner{}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--branch", "main", "--repo", "storefront", "--no-push", "--yes")
	require.NoError(t, err)

	assert.Empty(t, runner.ran(), "--no-push must not read or write the checkout")
	assert.Empty(t, terminal.ran())
	assert.Contains(t, out, "git remote add origin https://github.com/octo/storefront.git")
	assert.Contains(t, out, "git push --set-upstream origin main")
	// The branch is stated even here, so it is still what the new repository
	// deploys from — --no-push withholds the push, not the declaration.
	assert.Equal(t, "main", api.sentCreateBody()["production_branch"])
}

// The failure that produces no error anywhere: the push succeeds, the code
// lands, and nothing ever deploys. So it is not attempted, and the access to
// grant is reported instead.
func TestGitExportSkipsThePushWhenTheAppCannotSeeTheRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.createAppInstalled = false
	api.createInstallURL = "https://github.com/apps/volcano/installations/new"
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.NoError(t, err, "the repository and the binding both exist; this is not a failure")

	assert.Empty(t, terminal.ran(), "a push that deploys nothing must not be presented as progress")
	assert.Contains(t, out, "could not be confirmed to have access")
	assert.Contains(t, out, "https://github.com/apps/volcano/installations/new")
	// What a push deploys is said after the warning and in the future tense.
	// Above it, in the present tense, it read as a flat contradiction: one line
	// promising functions deploy, the next saying a push deploys nothing.
	assert.NotContains(t, out, "A push to main deploys:")
	assert.Contains(t, out, "Once it can, a push to main deploys: functions")
	// The remote is still recorded: it is what the user pushes with once access
	// is granted, and re-running create is not an option.
	assert.Contains(t, runner.ran(), "git remote add -- origin https://github.com/octo/storefront.git")
	assert.Contains(t, out, "git push --set-upstream origin main")
	assert.NotContains(t, out, "git remote add origin")
}

// The repository exists and the project is bound by the time the push runs, so a
// push failure may not read as "nothing happened".
func TestGitExportReportsTheRepositoryWhenThePushFails(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{err: errors.New("exit status 128")}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "octo/storefront is connected")
	assert.Contains(t, err.Error(), "the local step did not finish")
	// And the report still describes what exists, so the user is not left
	// guessing whether the repository is there.
	assert.Contains(t, out, "Created octo/storefront")
	assert.NotContains(t, out, "Pushed")
}

// The create's own push names its remote, so it always lands. What does not is
// everything after: --set-upstream writes branch.<name>.remote, which git
// consults last, behind pushRemote and pushDefault. A checkout with either set
// keeps pushing elsewhere, the new repository stops receiving commits, and the
// project stops deploying with nothing failing anywhere.
func TestGitExportWarnsWhenABarePushWouldGoElsewhere(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.outputs["git config --get remote.pushDefault"] = "upstream\n"
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.NoError(t, err, "the create and its push both succeeded; this is a warning about the next one")

	assert.Contains(t, out, "Pushed main to octo/storefront")
	assert.Contains(t, out, "remote.pushDefault")
	assert.Contains(t, out, "upstream")
	assert.Contains(t, out, "will not reach the new repository")
	assert.Contains(t, out, "git push origin main")
}

// Routing that already points at the remote being created needs no warning: the
// upstream and the configuration agree.
func TestGitExportStaysQuietWhenTheRoutingAgrees(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.outputs["git config --get remote.pushDefault"] = "origin\n"
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.NoError(t, err)
	assert.NotContains(t, out, "will not reach the new repository")
}

// These keys hold either a remote name or a URL, and a CI rewrite routinely
// leaves a job token in one of them.
func TestGitExportRedactsACredentialInThePushRouting(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.outputs["git config --get branch.main.pushRemote"] = "https://x-access-token:SECRETTOKEN@github.com/octo/elsewhere.git\n"
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.NoError(t, err)

	assert.Contains(t, out, "will not reach the new repository")
	assert.NotContains(t, out, "SECRETTOKEN")
}

// A push configuration git itself would fail on is not a reason to fail a create
// that already succeeded — but it is a reason not to claim the routing is fine.
func TestGitExportSaysSoWhenTheRoutingCannotBeRead(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.outputs["git config --get remote.pushDefault"] = "   \n"
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.NoError(t, err)
	assert.Contains(t, out, "Pushed main to octo/storefront")
	assert.Contains(t, out, "push configuration could not be read")
}

func TestGitExportPublicRepositorySendsPrivateFalse(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	_, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--branch", "main", "--repo", "storefront", "--public", "--yes")
	require.NoError(t, err)
	assert.Equal(t, false, api.sentCreateBody()["private"])
}

func TestGitExportRefusesPrivateAndPublicTogether(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
		"export", "--branch", "main", "--repo", "storefront", "--private", "--public", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--private and --public cannot be combined")
	assert.Zero(t, api.createAttempts())
}

func TestGitExportSendsAnOwnerTheAppIsInstalledOn(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.installationsByConnection[gitConnectionID] = append(
		api.installationsByConnection[gitConnectionID], installation(otherInstall, "acme", "Organization", "all"))
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/acme/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--branch", "main", "--repo", "acme/storefront", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "acme", api.sentCreateBody()["owner"])
	assert.Contains(t, out, "Created acme/storefront")
}

// No GitHub account connected at all — the commonest reason a create cannot
// work, and the one the CLI cannot fix: connecting an account is a browser
// redirect, so it happens in the dashboard. Refused before anything is created,
// and before the user is asked to confirm.
func TestGitExportRefusesWhenNoGitHubAccountIsConnected(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.connections = nil

	out, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no GitHub account is connected")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
	assert.Zero(t, api.createAttempts())
	assert.NotContains(t, out, "Create it", "the prompt must not be reached")
}

// Checked even without --owner, which used to skip the connection read entirely
// and leave the platform's 404 to explain it after the fact.
func TestGitExportChecksTheConnectionWithoutAnOwner(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	_, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.NoError(t, err)
	// The connection was read, and no installation listing was needed for it: an
	// unnamed owner is the platform's to resolve.
	assert.Equal(t, 1, api.connectionListings())
	assert.Zero(t, api.installationListings())
}

// The platform refuses this too, with a 404 that cannot say which accounts would
// have worked. This can, because it has the installation list in hand.
//
// No --yes, so this also pins the order: the check runs before the prompt. Asking
// the user to confirm a create that has already been established to be impossible
// gets a "yes" for nothing, and it is the prompt guarding the one action here
// that cannot be undone.
func TestGitExportRefusesAnOwnerTheAppIsNotInstalledOn(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	out, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"export", "--branch", "main", "--repo", "acme/storefront")
	require.Error(t, err)
	assert.NotContains(t, out, "Create it", "the prompt must not be reached")
	assert.Contains(t, err.Error(), "not installed on that account: acme")
	assert.Contains(t, err.Error(), "It is installed on: octo")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
	assert.Zero(t, api.createAttempts())
}

// Retrying under a new name is the wrong reflex here: the repository may already
// be on GitHub, and a retry leaves the user owning two.
func TestGitExportWarnsThatARepositoryMayExistOnConflict(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.createStatus = 409
	api.createErrorMessage = "octo/storefront was created but could not be connected"
	runner := checkoutRunner("main", "")

	_, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "may have been created on GitHub")
	assert.Contains(t, err.Error(), "octo/storefront was created but could not be connected")
	assert.Contains(t, err.Error(), "Do not create another one under a different name")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
	assert.NotContains(t, runner.ran(), "git remote add -- origin https://github.com/octo/storefront.git",
		"no remote may be recorded for a repository that may not be bound")
}

// The worst of the ambiguous outcomes: the request arrived, the repository may
// have been created, and no status came back to classify. Reported as a plain
// failure it would send the caller straight into a retry under a new name.
func TestGitExportWarnsThatARepositoryMayExistWhenNoAnswerArrives(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.createHangsUp = true
	runner := checkoutRunner("main", "")

	_, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.Error(t, err)
	assert.Equal(t, 1, api.createAttempts(), "the request did reach the platform")
	assert.Contains(t, err.Error(), "may have been created on GitHub")
	assert.Contains(t, err.Error(), "Do not create another one under a different name")
	assert.NotContains(t, runner.ran(), "git remote add -- origin https://github.com/octo/storefront.git")
}

// git refuses a remote name with whitespace, and it would refuse it after the
// create — leaving a repository whose remote never got recorded.
func TestGitExportRefusesARemoteNameWithWhitespace(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), nil, "",
		"export", "--branch", "main", "--repo", "storefront", "--remote", "foo ", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contains whitespace")
	assert.Zero(t, api.createAttempts())
}

// The names git refuses are more than the flag check can state — a leading dot,
// "..", "~", a ".lock" suffix — so git is asked, and asked before the create.
func TestGitExportRefusesARemoteNameGitRejects(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")

	_, err := executeGitCommandWith(t, api.serve(), runner, nil, "",
		"export", "--branch", "main", "--repo", "storefront", "--remote", "a~b", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `git will not accept "a~b" as a remote name`)
	assert.Contains(t, runner.ran(), "git check-ref-format --allow-onelevel a~b")
	assert.Zero(t, api.createAttempts())
}

func TestGitExportRefusesARemoteNameGitWouldReadAsAnOption(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
		"export", "--branch", "main", "--repo", "storefront", "--remote", "-x", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git reads it as an option")
	assert.Zero(t, api.createAttempts())
}

// git accepts "$", ";" and backticks in a branch name, so a command printed for
// the user to copy has to survive their shell unchanged. Printed bare,
// "topic$(id)" would run id instead of pushing topic.
func TestGitExportQuotesTheCommandsItPrints(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.createAppInstalled = false
	runner := checkoutRunner("topic$(id)", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "topic$(id)", "--yes")
	require.NoError(t, err)

	assert.Contains(t, out, `git push --set-upstream origin 'topic$(id)'`)
	assert.NotContains(t, out, "git push --set-upstream origin topic$(id)\n")
	// An ordinary name is not dressed up in quotes it does not need.
	assert.NotContains(t, out, "'origin'")
}

func TestShellQuote(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ value, want string }{
		{"main", "main"},
		{"release/2.0", "release/2.0"},
		{"https://github.com/octo/storefront.git", "https://github.com/octo/storefront.git"},
		{"topic$(id)", `'topic$(id)'`},
		{"a;b", `'a;b'`},
		{"a`id`b", "'a`id`b'"},
		{"a|b", `'a|b'`},
		{"~branch", `'~branch'`},
		// zsh expands a word starting with "=" to a command's path, and git
		// accepts "=foo" as a branch name.
		{"=foo", `'=foo'`},
		{"", `''`},
		// The one escape a single-quoted word needs.
		{"it's", `'it'\''s'`},
	} {
		assert.Equal(t, tc.want, shellQuote(tc.value), "quoting %q", tc.value)
	}
}

func TestGitExportRefusesAnAbsoluteRootDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
		"export", "--branch", "main", "--repo", "storefront", "--root-directory", "/etc", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an absolute path")
	assert.Zero(t, api.createAttempts())
}

// connect accepts an empty --root-directory because it has a previous value to
// reset. A repository being created has none, so a value that trims to nothing is
// a mistyped path or an unset variable — and it used to pass silently, binding an
// irreversible create to the repository root.
func TestGitExportRefusesAWhitespaceRootDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)

	_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
		"export", "--branch", "main", "--repo", "storefront", "--root-directory", "   ", "--yes")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only whitespace")
	assert.Zero(t, api.createAttempts())
}

// The commonest first failure of this command: GitHub was never connected. The
// platform answers 404 for that, for a missing project, and for an App not
// installed on the owner — two of the three are fixed in the dashboard, so the
// link has to be there. Without it this was the only git subcommand that left the
// user with a bare HTTP status.
func TestGitExportPointsAtTheDashboardWhenNothingWasFound(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	api.createStatus = 404
	api.createErrorMessage = "No GitHub account is connected"

	_, err := executeGitCommandWith(t, api.serve(), checkoutRunner("main", ""), &gitTerminalRunner{}, "",
		"export", "--repo", "storefront", "--branch", "main", "--yes")
	require.Error(t, err)
	// The platform's own message survives: it is the only thing that says which
	// of the three 404s this is.
	assert.Contains(t, err.Error(), "No GitHub account is connected")
	assert.Contains(t, err.Error(), "https://volcano.test/dashboard/project-settings/git")
	// Nothing was created, so nothing may suggest a repository might exist.
	assert.NotContains(t, err.Error(), "may have been created")
}

func TestGitExportSendsTheRootDirectory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--branch", "main", "--repo", "storefront", "--root-directory", "apps/api", "--yes")
	require.NoError(t, err)

	assert.Equal(t, "apps/api", api.sentCreateBody()["root_directory"])
	assert.Contains(t, out, "apps/api")
}

func TestGitExportRecordsAnSSHRemoteWhenAsked(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin git@github.com:octo/storefront.git")

	_, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "",
		"export", "--branch", "main", "--repo", "storefront", "--ssh", "--yes")
	require.NoError(t, err)
	assert.Contains(t, runner.ran(), "git remote add -- origin git@github.com:octo/storefront.git")
}

// Declining creates nothing and exits 0, as everywhere else in the CLI. The
// prompt matters more here than for any other git subcommand: it is the only one
// whose effect Volcano cannot undo.
func TestGitExportDeclinedCreatesNothing(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")

	out, err := executeGitCommandWith(t, api.serve(), runner, &gitTerminalRunner{}, "n\n",
		"export", "--repo", "storefront", "--branch", "main")
	require.NoError(t, err)

	assert.Zero(t, api.createAttempts())
	assert.NotContains(t, runner.ran(), "git remote add -- origin https://github.com/octo/storefront.git")
	assert.Contains(t, out, "cannot be undone")
}

// A repository that exists but has no branches is the same job as one this
// command created: the project's history becomes its history.
func TestGitExportBindsAnExistingEmptyRepository(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("trunk", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{} // ls-remote prints nothing: no branches

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--repo", "octo/storefront", "--branch", "trunk", "--yes")
	require.NoError(t, err)

	assert.Zero(t, api.createAttempts(), "the repository is already there; creating one would be a duplicate")
	assert.NotNil(t, api.sentConnectBody(), "it has to be bound")
	// The bind cannot carry a non-default branch, so an empty repository — whose
	// recorded branch is only the account's configured name — is corrected after.
	assert.Equal(t, "trunk", api.sentProductionBranch()["production_branch"])
	assert.Equal(t, []string{"git push --set-upstream -- origin trunk"}, terminal.ran()[1:])
	assert.Contains(t, out, "Connected octo/storefront")
	assert.NotContains(t, out, "Created octo/storefront")
}

// The case nobody else handles: unrelated histories. A push to the production
// branch would be refused and forcing it would discard what is there, so the
// project goes to a branch of its own and waits for a merge.
func TestGitExportPushesToASideBranchWhenTheRepositoryHasHistory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	runner.allow("git remote add -- origin https://github.com/octo/storefront.git")
	terminal := &gitTerminalRunner{lsRemote: remoteHistory}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "",
		"export", "--repo", "octo/storefront", "--branch", "main", "--yes")
	require.NoError(t, err)

	assert.Zero(t, api.createAttempts())
	assert.NotNil(t, api.sentConnectBody())
	// The production branch stays the repository's own: that is where the pull
	// request lands, and therefore what should deploy.
	assert.Nil(t, api.sentProductionBranch(), "the production branch must not be repointed at the side branch")
	assert.Equal(t, []string{"git push -- origin main:refs/heads/volcano/export"}, terminal.ran()[1:])

	assert.Contains(t, out, "Pushed to volcano/export on octo/storefront")
	assert.Contains(t, out, "already has its own history")
	assert.Contains(t, out, "Nothing deploys until that merge lands")
	assert.Contains(t, out,
		"https://github.com/octo/storefront/compare/main...volcano/export?expand=1")
	// Claiming a push deploys would be false here, so that line is withheld.
	assert.NotContains(t, out, "A push to main deploys:")
}

// The consequence is stated before the user agrees to it, not discovered in the
// report afterwards.
func TestGitExportWarnsBeforeBindingARepositoryWithHistory(t *testing.T) {
	setGitCommandTestHome(t)
	api := newGitAPI(t)
	runner := checkoutRunner("main", "")
	terminal := &gitTerminalRunner{lsRemote: remoteHistory}

	out, err := executeGitCommandWith(t, api.serve(), runner, terminal, "n\n",
		"export", "--repo", "octo/storefront", "--branch", "main")
	require.NoError(t, err)

	assert.Contains(t, out, "has its own history")
	assert.Contains(t, out, "Nothing deploys until you merge that branch")
	assert.Nil(t, api.sentConnectBody(), "a declined prompt binds nothing")
	assert.Zero(t, api.createAttempts())
}

func TestGitExportRefusesRepoShapesThatNameNothing(t *testing.T) {
	setGitCommandTestHome(t)
	for _, tc := range []struct{ repo, want string }{
		{"a/b/c", "too many parts"},
		{"/storefront", "names no account"},
		{"octo/", "names no repository"},
		{"   ", "names no repository"},
		{"my app", "which GitHub does not allow"},
	} {
		api := newGitAPI(t)
		_, err := executeGitCommandWith(t, api.serve(), nil, nil, "",
			"export", "--repo", tc.repo, "--branch", "main", "--yes")
		require.Errorf(t, err, "--repo %q", tc.repo)
		assert.Containsf(t, err.Error(), tc.want, "--repo %q", tc.repo)
		assert.Zerof(t, api.createAttempts(), "--repo %q", tc.repo)
	}
}
