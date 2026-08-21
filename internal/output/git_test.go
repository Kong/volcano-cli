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

func ptr(s string) *string { return &s }

// renderExport drives the surviving renderer. These tests exist for the shared
// binding and deploy-settings rendering, which it uses unchanged; only the
// headline differs from the connect renderer they were written against.
func renderExport(w *bytes.Buffer, binding GitBinding) {
	GitCreated(w, GitCreation{Binding: binding, Created: true, AppInstalled: true})
}

// frontend_app_root is documented as "Omitted for the repo root", so a frontend
// that builds from the repository root arrives with a nil app root. That is the
// ordinary case, not an edge case.
func TestGitExportRenderRendersAFrontendWithNoAppRoot(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(true, false, ptr("web"), nil)})

	assert.Contains(t, out.String(), "A push to main deploys: frontend web")
	assert.NotContains(t, out.String(), "()")
}

func TestGitExportRenderRendersEveryDeployTargetCombination(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		settings *apiclient.ProjectGitDeploySettings
		want     string
	}{
		"functions only":       {gitSettingsFixture(true, true, nil, nil), "functions"},
		"frontend only":        {gitSettingsFixture(true, false, ptr("web"), nil), "frontend web"},
		"frontend with a root": {gitSettingsFixture(true, false, ptr("web"), ptr("apps/web")), "frontend web (apps/web)"},
		"both":                 {gitSettingsFixture(true, true, ptr("web"), ptr("apps/web")), "functions, frontend web (apps/web)"},
		// The update contract permits an empty frontend name to mean "no
		// frontend", so an echoed-back empty string must not render one.
		"empty frontend name": {gitSettingsFixture(true, true, ptr(""), nil), "functions"},
		"empty app root":      {gitSettingsFixture(true, false, ptr("web"), ptr("")), "frontend web"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			renderExport(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: tc.settings, Project: "Storefront (3333)"})
			// The whole line, terminator included: a substring match would pass
			// against a trailing empty frontend or a bare "()".
			assert.Contains(t, out.String(), "A push to main deploys: "+tc.want+"\n")
		})
	}
}

// Auto-deploy off is the whole point of printing this: the binding looks
// complete and a push still does nothing. Saying so and then also listing what
// a push deploys would contradict itself.
func TestGitExportRenderWarnsAutoDeployOffAndNamesNoTargets(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(false, true, ptr("web"), nil)})

	assert.Contains(t, out.String(), "Auto-deploy is off, so a push to main will not deploy anything.")
	assert.NotContains(t, out.String(), "A push to main deploys:")
}

func TestGitExportRenderWarnsWhenAutoDeployHasNothingSelected(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: gitSettingsFixture(true, false, nil, nil), Project: "Storefront (3333)"})

	assert.Contains(t, out.String(), "neither functions nor a frontend is selected")
	assert.NotContains(t, out.String(), "A push to main deploys:")
}

// Reading the settings back is allowed to fail without failing the connect, so
// the renderer has to tolerate their absence rather than panic just after the
// bind landed.
func TestGitExportRenderToleratesUnreadableSettings(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: nil, Project: "Storefront (3333)"})

	assert.Contains(t, out.String(), "Created octo/storefront")
	assert.NotContains(t, out.String(), "A push to")
	assert.NotContains(t, out.String(), "Auto-deploy")
}

// "Nothing to say about what a push deploys" and "could not find out" are
// different states, and only one of them is safe to read as "no deploy
// configured". A failed read says so instead of looking like the former.
func TestGitExportRenderWarnsWhenTheSettingsCouldNotBeRead(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{
		Connection:  gitConnectionFixture(""),
		SettingsErr: errors.New("HTTP 500: internal error"),
	})

	assert.Contains(t, out.String(), "Created octo/storefront")
	assert.Contains(t, out.String(), "deploy settings could not be read")
	assert.Contains(t, out.String(), "what a push to main deploys is unknown")
	assert.Contains(t, out.String(), "HTTP 500: internal error")
}

// A binding that was only read back carries no settings and no failure; there
// is nothing to warn about.
func TestGitExportRenderStaysQuietWhenThereWasNoSettingsRead(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{Connection: gitConnectionFixture("")})

	assert.NotContains(t, out.String(), "could not be read")
}

func TestGitExportRenderOmitsAnEmptyRootDirectory(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{Connection: gitConnectionFixture(""), Settings: nil, Project: "Storefront (3333)"})
	assert.NotContains(t, out.String(), "Root directory")

	var withRoot bytes.Buffer
	renderExport(&withRoot, GitBinding{Connection: gitConnectionFixture("apps/api"), Settings: nil, Project: "Storefront (3333)"})
	assert.Contains(t, withRoot.String(), "Root directory: apps/api")
}

func TestGitExportRenderNamesTheProject(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{
		Connection: gitConnectionFixture(""),
		Project:    "Storefront (33333333-3333-4333-8333-333333333333)",
	})
	assert.Contains(t, out.String(), "Project: Storefront (33333333-3333-4333-8333-333333333333)")
}

// The connection decides whose stored GitHub token the platform reads the
// repository with on every future deploy, and more than one connection can reach
// the same repository.
func TestGitExportRenderNamesTheGitHubAccount(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	renderExport(&out, GitBinding{
		Connection:    gitConnectionFixture(""),
		GitHubAccount: "shared-bot",
	})
	assert.Contains(t, out.String(), "GitHub account: shared-bot")
}

func TestGitExportRenderReportsAnInstallationElsewhere(t *testing.T) {
	t.Parallel()
	// The repository's own owner is the ordinary case and stays unmentioned.
	var owned bytes.Buffer
	renderExport(&owned, GitBinding{
		Connection: gitConnectionFixture(""), InstallationAccount: "OCTO",
	})
	assert.NotContains(t, owned.String(), "App installed on")

	// An installation on another account is what the binding quietly depends on.
	var elsewhere bytes.Buffer
	renderExport(&elsewhere, GitBinding{
		Connection: gitConnectionFixture(""), InstallationAccount: "acme",
	})
	assert.Contains(t, elsewhere.String(), "App installed on: acme")
}
