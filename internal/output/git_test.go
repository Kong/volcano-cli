package output

import (
	"bytes"
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

// frontend_app_root is documented as "Omitted for the repo root", so a frontend
// that builds from the repository root arrives with a nil app root. That is the
// ordinary case, not an edge case.
func TestGitConnectedRendersAFrontendWithNoAppRoot(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, gitConnectionFixture(""), gitSettingsFixture(true, false, ptr("web"), nil))

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
			GitConnected(&out, gitConnectionFixture(""), tc.settings)
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
	GitConnected(&out, gitConnectionFixture(""), gitSettingsFixture(false, true, ptr("web"), nil))

	assert.Contains(t, out.String(), "Auto-deploy is off, so a push to main will not deploy anything.")
	assert.NotContains(t, out.String(), "A push to main deploys:")
}

func TestGitConnectedWarnsWhenAutoDeployHasNothingSelected(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, gitConnectionFixture(""), gitSettingsFixture(true, false, nil, nil))

	assert.Contains(t, out.String(), "neither functions nor a frontend is selected")
	assert.NotContains(t, out.String(), "A push to main deploys:")
}

// Reading the settings back is allowed to fail without failing the connect, so
// the renderer has to tolerate their absence rather than panic just after the
// bind landed.
func TestGitConnectedToleratesUnreadableSettings(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, gitConnectionFixture(""), nil)

	assert.Contains(t, out.String(), "Connected octo/storefront")
	assert.NotContains(t, out.String(), "A push to")
	assert.NotContains(t, out.String(), "Auto-deploy")
}

func TestGitConnectedOmitsAnEmptyRootDirectory(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnected(&out, gitConnectionFixture(""), nil)
	assert.NotContains(t, out.String(), "Root directory")

	var withRoot bytes.Buffer
	GitConnected(&withRoot, gitConnectionFixture("apps/api"), nil)
	assert.Contains(t, withRoot.String(), "Root directory: apps/api")
}

func TestGitConnectionReportsWithoutClaimingAChange(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	GitConnection(&out, gitConnectionFixture(""), nil)

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
