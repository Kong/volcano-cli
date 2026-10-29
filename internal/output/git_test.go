package output

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// These drive the renderer directly rather than through a command, because the
// inputs that matter most here are the ones a command's fake API cannot easily
// produce: absent optional fields, and settings that could not be read at all.

func gitConnectionFixture(rootDirectory string) *apiclient.ProjectGitConnection {
	return &apiclient.ProjectGitConnection{
		RepoFullName:       "octo/storefront",
		RepoId:             90210,
		RepoInstallationId: 4242,
		ProductionBranch:   "main",
		RootDirectory:      rootDirectory,
		UpdatedAt:          time.Date(2026, time.August, 18, 0, 0, 0, 0, time.UTC),
	}
}

func gitSettingsFixture(autoDeploy, functions bool, frontend, appRoot *string) *apiclient.ProjectGitDeploySettings {
	return &apiclient.ProjectGitDeploySettings{
		AutoDeployEnabled: autoDeploy,
		DeployFunctions:   functions,
		FrontendName:      frontend,
		FrontendAppRoot:   appRoot,
		UpdatedAt:         time.Date(2026, time.August, 18, 0, 0, 0, 0, time.UTC),
	}
}

// frontend_app_root is documented as "Omitted for the repo root", so a frontend
// that builds from the repository root arrives with a nil app root. That is the
// ordinary case, not an edge case.
func TestGitConnectedRendersAFrontendWithNoAppRoot(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(true, false, new("web"), nil)})

	assert.Contains(t, out.String(), "A push to main deploys: frontend web")
	assert.NotContains(t, out.String(), "()")
}

func TestGitConnectedRendersEveryDeployTargetCombination(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		settings *apiclient.ProjectGitDeploySettings
		want     string
	}{
		"functions only":       {gitSettingsFixture(true, true, nil, nil), "functions"},
		"frontend only":        {gitSettingsFixture(true, false, new("web"), nil), "frontend web"},
		"frontend with a root": {gitSettingsFixture(true, false, new("web"), new("apps/web")), "frontend web (apps/web)"},
		"both":                 {gitSettingsFixture(true, true, new("web"), new("apps/web")), "functions, frontend web (apps/web)"},
		// The update contract permits an empty frontend name to mean "no
		// frontend", so an echoed-back empty string must not render one.
		"empty frontend name": {gitSettingsFixture(true, true, new(""), nil), "functions"},
		"empty app root":      {gitSettingsFixture(true, false, new("web"), new("")), "frontend web"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			GitConnected(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: tc.settings, Project: "Storefront (3333)"})
			// The whole line, terminator included: a substring match would pass
			// against a trailing empty frontend or a bare "()".
			assert.Contains(t, out.String(), "A push to main deploys: "+tc.want+"\n")
		})
	}
}

// Auto-deploy off is the whole point of printing this: the binding looks
// complete and a push still does nothing. Saying so and then also listing what
// a push deploys would contradict itself.
func TestGitConnectedWarnsAutoDeployOffAndNamesNoTargets(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(false, true, new("web"), nil)})

	assert.Contains(t, out.String(), "Auto-deploy is off, so a push to main will not deploy anything.")
	assert.NotContains(t, out.String(), "A push to main deploys:")
}

func TestGitConnectedWarnsWhenAutoDeployHasNothingSelected(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(true, false, nil, nil), Project: "Storefront (3333)"})

	assert.Contains(t, out.String(), "neither functions nor a frontend is selected")
	assert.NotContains(t, out.String(), "A push to main deploys:")
}

// Reading the settings back is allowed to fail without failing the connect, so
// the renderer has to tolerate their absence rather than panic just after the
// bind landed.
func TestGitConnectedToleratesUnreadableSettings(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: nil, Project: "Storefront (3333)"})

	assert.Contains(t, out.String(), "Connected octo/storefront")
	assert.NotContains(t, out.String(), "A push to")
	assert.NotContains(t, out.String(), "Auto-deploy")
}

// "Nothing to say about what a push deploys" and "could not find out" are
// different states, and only one of them is safe to read as "no deploy
// configured". A failed read says so instead of looking like the former.
func TestGitConnectedWarnsWhenTheSettingsCouldNotBeRead(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{
		Connection:  gitConnectionFixture(""),
		SettingsErr: errors.New("HTTP 500: internal error"),
	})

	assert.Contains(t, out.String(), "Connected octo/storefront")
	assert.Contains(t, out.String(), "deploy settings could not be read")
	assert.Contains(t, out.String(), "what a push to main deploys is unknown")
	assert.Contains(t, out.String(), "HTTP 500: internal error")
}

// A binding that was only read back carries no settings and no failure; there
// is nothing to warn about.
func TestGitConnectedStaysQuietWhenThereWasNoSettingsRead(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{Connection: gitConnectionFixture("")})

	assert.NotContains(t, out.String(), "could not be read")
}

func TestGitConnectionWarnsWhenTheSettingsCouldNotBeRead(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnection(&out, GitBinding{
		Connection:  gitConnectionFixture(""),
		SettingsErr: errors.New("HTTP 503: unavailable"),
	})

	assert.Contains(t, out.String(), "already connected")
	assert.Contains(t, out.String(), "deploy settings could not be read")
}

func TestGitConnectedOmitsAnEmptyRootDirectory(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: nil, Project: "Storefront (3333)"})
	assert.NotContains(t, out.String(), "Root directory")

	var withRoot bytes.Buffer
	GitConnected(&withRoot, GitBinding{Connection: gitConnectionFixture("apps/api"), Settings: nil, Project: "Storefront (3333)"})
	assert.Contains(t, withRoot.String(), "Root directory: apps/api")
}

func TestGitConnectionReportsWithoutClaimingAChange(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnection(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: nil, Project: "Storefront (3333)"})

	assert.Contains(t, out.String(), "octo/storefront is already connected to this project.")
	assert.NotContains(t, out.String(), "Connected octo/storefront")
}

// "Disconnect" reads as destructive and is not, which is worth saying.
func TestGitDisconnectedSaysTheRepositoryIsUntouched(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitDisconnected(&out, "octo/storefront")

	assert.Contains(t, out.String(), "Disconnected octo/storefront")
	assert.Contains(t, out.String(), "The repository itself was not changed.")
}

// The repository comes from the working directory and the project comes from the
// CLI's configuration, so the project is chosen without the user naming it here.
// Not saying which one was bound is how a stale selection goes unnoticed.
func TestGitConnectedNamesTheProject(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{
		Connection: gitConnectionFixture(""),
		Project:    "Storefront (33333333-3333-4333-8333-333333333333)",
	})
	assert.Contains(t, out.String(), "Project: Storefront (33333333-3333-4333-8333-333333333333)")
}

// The connection decides whose stored GitHub token the platform reads the
// repository with on every future deploy, and more than one connection can reach
// the same repository.
func TestGitConnectedNamesTheGitHubAccount(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, GitBinding{
		Connection:    gitConnectionFixture(""),
		GitHubAccount: "shared-bot",
	})
	assert.Contains(t, out.String(), "GitHub account: shared-bot")
}

func TestGitConnectedReportsAnInstallationElsewhere(t *testing.T) {
	t.Parallel()
	// The repository's own owner is the ordinary case and stays unmentioned.
	var owned bytes.Buffer
	GitConnected(&owned, GitBinding{
		Connection: gitConnectionFixture(""), InstallationAccount: "OCTO",
	})
	assert.NotContains(t, owned.String(), "App installed on")

	// An installation on another account is what the binding quietly depends on.
	var elsewhere bytes.Buffer
	GitConnected(&elsewhere, GitBinding{
		Connection: gitConnectionFixture(""), InstallationAccount: "acme",
	})
	assert.Contains(t, elsewhere.String(), "App installed on: acme")
}

// The hint is opt-in: only a caller that knows the command's path can name it.
func TestGitDeploySettingsHintsAtAFrontendOnlyWhenAskedTo(t *testing.T) {
	t.Parallel()
	var without, with bytes.Buffer
	settings := gitSettingsFixture(true, true, nil, nil)
	GitStatus(&without, GitBinding{Connection: gitConnectionFixture(""), Settings: settings})
	GitStatus(&with, GitBinding{Connection: gitConnectionFixture(""), Settings: settings, FrontendCommand: "volcano git frontend"})

	assert.NotContains(t, without.String(), "To deploy a frontend")
	assert.Contains(t, with.String(), "A push to main deploys: functions\nTo deploy a frontend on push too, run volcano git frontend <name> --root <path>\n")
}

// With nothing selected, the frontend is one of the two things that would make
// a push deploy something, so the hint follows the warning.
func TestGitDeploySettingsHintsAtAFrontendWhenNothingIsSelected(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitStatus(&out, GitBinding{
		Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(true, false, nil, nil),
		FrontendCommand: "volcano git frontend",
	})

	assert.Contains(t, out.String(), "neither functions nor a frontend is selected")
	assert.Contains(t, out.String(), "To deploy a frontend on push too, run volcano git frontend <name> --root <path>")
}

// While auto-deploy is off nothing deploys, frontend or not, so the hint would
// point at the wrong fix.
func TestGitDeploySettingsGivesNoFrontendHintWhileAutoDeployIsOff(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitStatus(&out, GitBinding{
		Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(false, true, nil, nil),
		FrontendCommand: "volcano git frontend",
	})

	assert.Contains(t, out.String(), "Auto-deploy is off")
	assert.NotContains(t, out.String(), "To deploy a frontend")
}

func TestGitFrontendSetNamesTheFrontendAndWhatAPushDeploys(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitFrontendSet(&out, GitBinding{
		Connection: gitConnectionFixture("apps"), Settings: gitSettingsFixture(true, true, new("web"), new("site")),
		Project: "Storefront (3333)",
	})

	assert.Contains(t, out.String(), "Frontend set to web (site)")
	assert.Contains(t, out.String(), "Root directory: apps")
	assert.Contains(t, out.String(), "Nothing was redeployed. The next push to main deploys this frontend.")
	assert.Contains(t, out.String(), "A push to main deploys: functions, frontend web (site)\n")
}

func TestGitFrontendSetSaysWhenAPushDeploysNoFrontend(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitFrontendSet(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(true, true, nil, nil)})

	assert.Contains(t, out.String(), "Frontend cleared, so a push deploys no frontend")
	assert.Contains(t, out.String(), "A frontend already deployed stays up; pushes no longer redeploy it.")
	assert.NotContains(t, out.String(), "frontend web")
}
