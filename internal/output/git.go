package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// GitConnected renders a repository binding that was just made, followed by
// what a push to the production branch will actually deploy. Settings are
// optional: a connect still succeeded if reading them afterwards did not.
func GitConnected(w io.Writer, connection *apiclient.ProjectGitConnection, settings *apiclient.ProjectGitDeploySettings) {
	Success(w, "Connected %s", connection.RepoFullName)
	gitConnectionDetail(w, theme.On(w), connection)
	gitDeploySettings(w, connection, settings)
}

// GitConnection renders an existing binding without claiming anything changed.
func GitConnection(w io.Writer, connection *apiclient.ProjectGitConnection, settings *apiclient.ProjectGitDeploySettings) {
	on := theme.On(w)
	fmt.Fprintf(w, "%s is already connected to this project.\n", theme.Command(connection.RepoFullName, on))
	gitConnectionDetail(w, on, connection)
	gitDeploySettings(w, connection, settings)
}

// GitDisconnected renders a removed binding. The repository is untouched, which
// is worth saying: "disconnect" reads as destructive and is not.
func GitDisconnected(w io.Writer, repoFullName string) {
	Success(w, "Disconnected %s", repoFullName)
	Note(w, "The repository itself was not changed. Pushes to it no longer deploy.")
}

func gitConnectionDetail(w io.Writer, on bool, connection *apiclient.ProjectGitConnection) {
	kv(w, on, "Repository", "%s", connection.RepoFullName)
	kv(w, on, "Production branch", "%s", connection.ProductionBranch)
	if root := strings.TrimSpace(connection.RootDirectory); root != "" {
		kv(w, on, "Root directory", "%s", root)
	}
}

// gitDeploySettings says what a push does. Auto-deploy off is the silent
// failure worth surfacing here: the binding looks complete, and nothing
// happens on push.
func gitDeploySettings(w io.Writer, connection *apiclient.ProjectGitConnection, settings *apiclient.ProjectGitDeploySettings) {
	if settings == nil {
		return
	}

	on := theme.On(w)
	fmt.Fprintln(w)
	if !settings.AutoDeployEnabled {
		Warning(w, "Auto-deploy is off, so a push to %s will not deploy anything.", connection.ProductionBranch)
		return
	}

	targets := deployTargets(settings)
	if len(targets) == 0 {
		Warning(w, "Auto-deploy is on, but neither functions nor a frontend is selected, so a push deploys nothing.")
		return
	}
	fmt.Fprintf(w, "%s%s\n",
		theme.Dim(fmt.Sprintf("A push to %s deploys: ", connection.ProductionBranch), on),
		strings.Join(targets, ", "),
	)
}

func deployTargets(settings *apiclient.ProjectGitDeploySettings) []string {
	targets := make([]string, 0, 2)
	if settings.DeployFunctions {
		targets = append(targets, "functions")
	}
	if settings.FrontendName != nil && strings.TrimSpace(*settings.FrontendName) != "" {
		frontend := "frontend " + *settings.FrontendName
		if settings.FrontendAppRoot != nil && strings.TrimSpace(*settings.FrontendAppRoot) != "" {
			frontend += " (" + *settings.FrontendAppRoot + ")"
		}
		targets = append(targets, frontend)
	}
	return targets
}
