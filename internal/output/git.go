package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// GitBinding is everything worth reporting about a binding: what was bound,
// which project it was bound to, and which GitHub identity it was bound
// through. The project and the GitHub identity are chosen by the CLI rather
// than named by the user, which is why they are reported rather than assumed.
type GitBinding struct {
	Connection *apiclient.ProjectGitConnection
	Settings   *apiclient.ProjectGitDeploySettings
	// Project labels the project that was bound.
	Project string
	// GitHubAccount is the connected GitHub account whose stored token the
	// platform reads the repository with. Empty when it is not known, as it is
	// not for a binding that was only read back.
	GitHubAccount string
	// InstallationAccount is the account the App installation belongs to. It is
	// reported only when it differs from the repository's owner, which is the
	// case worth pointing out.
	InstallationAccount string
	// SettingsErr is why Settings could not be read, when they could not. The
	// binding is unaffected by that failure, so it is reported as a warning
	// rather than swallowed: "nothing to say about what a push deploys" and
	// "could not find out" are different states, and only one of them is safe
	// to read as "no deploy configured".
	SettingsErr error
	// GitCommand is the path of the git command group, such as "volcano git".
	// When it is set, the output names the command that changes what a push
	// deploys wherever that is the next step: a repository that deploys only
	// its functions looks complete, and a site that never deploys gives no
	// clue why.
	GitCommand string
	// Changed is the git subcommand that just ran, such as "frontend". Its own
	// next step is left out, since naming it would only repeat the command.
	Changed string
	// Source is the project's source of truth, which only status reports. Nil
	// with no SourceErr means the API has no source export to report on.
	Source    *apiclient.ProjectSourceExportState
	SourceErr error
}

// GitConnected renders a repository binding that was just made, followed by
// what a push to the production branch will actually deploy. Settings are
// optional: a connect still succeeded if reading them afterwards did not.
func GitConnected(w io.Writer, binding GitBinding) {
	Success(w, "Connected %s", binding.Connection.RepoFullName)
	gitBindingDetail(w, theme.On(w), binding)
	gitDeploySettings(w, binding)
}

// GitConnection renders an existing binding without claiming anything changed.
func GitConnection(w io.Writer, binding GitBinding) {
	fmt.Fprintf(w, "%s is already connected to this project.\n", binding.Connection.RepoFullName)
	gitBindingDetail(w, theme.On(w), binding)
	gitDeploySettings(w, binding)
}

// GitStatus renders a binding as a plain report. It deliberately does not read
// as an outcome — nothing was just done — and it names no GitHub account,
// because answering that would mean contacting the provider, which the command
// behind it does not do. It lists every deploy setting, each of which a git
// subcommand changes.
func GitStatus(w io.Writer, binding GitBinding) {
	on := theme.On(w)
	gitBindingDetail(w, on, binding)
	switch {
	case binding.Source != nil:
		kv(w, on, "Source of truth", "%s", sourceOfTruth(binding.Source.Mode))
	case binding.SourceErr != nil:
		kv(w, on, "Source of truth", "unknown: %v", binding.SourceErr)
	}
	if settings := binding.Settings; settings != nil {
		kv(w, on, "Auto-deploy", "%s", onOff(settings.AutoDeployEnabled))
		kv(w, on, "Functions", "%s", onOff(settings.DeployFunctions))
		frontend := frontendTarget(settings)
		if frontend == "" {
			frontend = "none"
		}
		kv(w, on, "Frontend", "%s", frontend)
	}
	gitDeploySettings(w, binding)
	if source := binding.Source; source != nil &&
		(source.Mode == apiclient.ProjectSourceExportStateModeGitExporting ||
			source.Mode == apiclient.ProjectSourceExportStateModeGitPending) {
		nextStep(w, on, binding, "Direct deploys are paused until it finishes. To resume them, run ", "export", "cancel")
	}
}

// sourceOfTruth says what deploys a project's code. A project Volcano owns
// deploys from both the CLI and pushes; once the repository owns it, only
// pushes deploy code.
func sourceOfTruth(mode apiclient.ProjectSourceExportStateMode) string {
	switch mode {
	case apiclient.ProjectSourceExportStateModePlatform:
		return "Volcano (direct deploys and pushes both deploy)"
	case apiclient.ProjectSourceExportStateModeGitExporting:
		return "moving to the repository: the export is pushing its first commit"
	case apiclient.ProjectSourceExportStateModeGitPending:
		return "moving to the repository: the export's deployment is running"
	case apiclient.ProjectSourceExportStateModeGit:
		return "the repository (only pushes deploy code)"
	default:
		return string(mode)
	}
}

// GitSourceExported renders the first commit an export pushed, what it could
// not carry, and what happens next.
func GitSourceExported(w io.Writer, result *apiclient.ProjectSourceExport, statusCommand string) {
	on := theme.On(w)
	files := "files"
	if result.FileCount == 1 {
		files = "file"
	}
	Success(w, "Exported %d %s to %s", result.FileCount, files, result.RepoFullName)
	kv(w, on, "Branch", "%s", result.Branch)
	kv(w, on, "Commit", "%s", result.CommitSha)
	if len(result.Skipped) > 0 {
		fmt.Fprintln(w)
		Warning(w, "Skipped, with no successful deployment to take source from:")
		for _, skip := range result.Skipped {
			fmt.Fprintf(w, "  %s %s: %s\n", skip.Kind, skip.Name, skip.Reason)
		}
	}
	if len(result.Omitted) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, theme.Dim("Left out of the commit:", on))
		for _, omission := range result.Omitted {
			line := omission.Kind + " " + omission.Path
			if omission.Resource != "" {
				line += " (" + omission.Resource + ")"
			}
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	fmt.Fprintln(w)
	Note(w, "The push is deploying. When it succeeds, the repository is the project's source of truth. "+
		"Follow it with %s.", statusCommand)
}

// GitSourceExportCanceled renders a canceled export, and whether export can
// still run: once its first commit was made, it cannot.
func GitSourceExportCanceled(w io.Writer, consumed bool) {
	Success(w, "Canceled the source export. Direct deploys work again.")
	if consumed {
		Note(w, "The repository and its first commit were not changed. Export cannot run again for this project.")
		return
	}
	Note(w, "The repository was not changed.")
}

// GitNotConnected renders a project with no repository bound. This is a state,
// not a failure, so it reads as an answer and says what would change it.
func GitNotConnected(w io.Writer, project, connectCommand string) {
	on := theme.On(w)
	kv(w, on, "Project", "%s", project)
	fmt.Fprintf(w, "%s\n", theme.Dim("No repository is connected, so pushes do not deploy.", on))
	fmt.Fprintf(w, "\n%s%s\n", theme.Dim("Connect one with ", on), theme.Command(connectCommand, on))
}

// GitRootDirectorySet renders a root directory that was just set, followed by
// what a push to the production branch deploys. Nothing is redeployed by it,
// which is worth saying: the change only shows on the next deploy, and only
// the deploy settings say whether a push makes one.
func GitRootDirectorySet(w io.Writer, binding GitBinding) {
	connection := binding.Connection
	if root := strings.TrimSpace(connection.RootDirectory); root != "" {
		Success(w, "Root directory set to %s", root)
	} else {
		Success(w, "Root directory reset to the repository root")
	}
	gitBindingDetail(w, theme.On(w), binding)
	Note(w, "Nothing was redeployed. The next deploy from the repository builds from this directory.")
	gitDeploySettings(w, binding)
}

// GitFrontendSet renders the frontend a push now deploys, or that it deploys
// none, followed by what a push to the production branch deploys. Like a root
// directory change, nothing is redeployed by it.
func GitFrontendSet(w io.Writer, binding GitBinding) {
	frontend := frontendTarget(binding.Settings)
	if frontend != "" {
		Success(w, "Frontend set to %s", frontend)
	} else {
		Success(w, "Frontend cleared, so a push deploys no frontend")
	}
	gitBindingDetail(w, theme.On(w), binding)
	switch {
	case frontend == "":
		Note(w, "Nothing was removed. A frontend already deployed stays up; pushes no longer redeploy it.")
	case binding.Settings.AutoDeployEnabled:
		Note(w, "Nothing was redeployed. The next push to %s deploys this frontend.",
			binding.Connection.ProductionBranch)
	default:
		// Auto-deploy off: the settings below say no push deploys anything, so
		// promising this one would contradict them.
		Note(w, "Nothing was redeployed.")
	}
	gitDeploySettings(w, binding)
}

// GitAutoDeploySet renders auto-deploy just turned on or off, followed by what
// a push to the production branch deploys.
func GitAutoDeploySet(w io.Writer, binding GitBinding) {
	branch := binding.Connection.ProductionBranch
	Success(w, "Auto-deploy turned %s", onOff(binding.Settings.AutoDeployEnabled))
	gitBindingDetail(w, theme.On(w), binding)
	switch {
	case !binding.Settings.AutoDeployEnabled:
		Note(w, "Nothing was removed. What is already deployed stays up; pushes to %s no longer deploy.", branch)
	case len(deployTargets(binding.Settings)) == 0:
		// Nothing selected: the settings below say a push deploys nothing and
		// name the command that selects something, so promising pushes would
		// contradict them.
		Note(w, "Nothing was deployed.")
	default:
		Note(w, "Nothing was deployed. Pushes to %s deploy from now on.", branch)
	}
	gitDeploySettings(w, binding)
}

// GitFunctionsSet renders deploying functions on push just turned on or off,
// followed by what a push to the production branch deploys.
func GitFunctionsSet(w io.Writer, binding GitBinding) {
	settings := binding.Settings
	Success(w, "Functions turned %s", onOff(settings.DeployFunctions))
	gitBindingDetail(w, theme.On(w), binding)
	switch {
	case !settings.DeployFunctions:
		Note(w, "Nothing was removed. Functions already deployed stay up; pushes no longer redeploy them.")
	case settings.AutoDeployEnabled:
		Note(w, "Nothing was deployed. The next push to %s deploys the repository's functions.",
			binding.Connection.ProductionBranch)
	default:
		// Auto-deploy off: the settings below say no push deploys anything, so
		// promising this one would contradict them.
		Note(w, "Nothing was deployed.")
	}
	gitDeploySettings(w, binding)
}

// GitProductionBranchSet renders the branch a push must now land on, and
// whether it follows the repository's default branch or is pinned.
func GitProductionBranchSet(w io.Writer, binding GitBinding) {
	connection := binding.Connection
	if connection.ProductionBranchFollowsDefault {
		Success(w, "Production branch follows the repository's default branch, %s", connection.ProductionBranch)
	} else {
		Success(w, "Production branch set to %s", connection.ProductionBranch)
	}
	gitBindingDetail(w, theme.On(w), binding)
	if connection.ProductionBranchFollowsDefault {
		Note(w, "Renaming the default branch on GitHub moves the project with it. Nothing was deployed.")
	} else {
		Note(w, "Pinned: renaming the default branch on GitHub no longer moves it. Nothing was deployed.")
	}
	gitDeploySettings(w, binding)
}

// GitDisconnected renders a removed binding. The repository is untouched, which
// is worth saying: "disconnect" reads as destructive and is not.
func GitDisconnected(w io.Writer, repoFullName string) {
	Success(w, "Disconnected %s", repoFullName)
	Note(w, "The repository itself was not changed. Pushes to it no longer deploy.")
}

func gitBindingDetail(w io.Writer, on bool, binding GitBinding) {
	connection := binding.Connection
	if binding.Project != "" {
		kv(w, on, "Project", "%s", binding.Project)
	}
	kv(w, on, "Repository", "%s", connection.RepoFullName)
	kv(w, on, "Production branch", "%s (%s)", connection.ProductionBranch, branchSource(connection))
	if root := strings.TrimSpace(connection.RootDirectory); root != "" {
		kv(w, on, "Root directory", "%s", root)
	}
	if binding.GitHubAccount != "" {
		kv(w, on, "GitHub account", "%s", binding.GitHubAccount)
	}
	// The installation is usually the repository's own owner; saying so every
	// time is noise, and saying nothing when it is not hides that the binding
	// depends on an installation somewhere else.
	if account := binding.InstallationAccount; account != "" &&
		!strings.EqualFold(account, repositoryOwner(connection.RepoFullName)) {
		kv(w, on, "App installed on", "%s", account)
	}
}

// branchSource says why a push to the production branch is what deploys: the
// branch moves with the repository's default branch, or the project pinned it.
func branchSource(connection *apiclient.ProjectGitConnection) string {
	if connection.ProductionBranchFollowsDefault {
		return "follows the default branch"
	}
	return "pinned"
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func repositoryOwner(fullName string) string {
	owner, _, _ := strings.Cut(fullName, "/")
	return owner
}

// gitDeploySettings says what a push does. Auto-deploy off is the silent
// failure worth surfacing here: the binding looks complete, and nothing
// happens on push.
func gitDeploySettings(w io.Writer, binding GitBinding) {
	connection, settings := binding.Connection, binding.Settings
	if settings == nil {
		if binding.SettingsErr != nil {
			fmt.Fprintln(w)
			Warning(w, "The connection is in place, but its deploy settings could not be read, "+
				"so what a push to %s deploys is unknown: %v", connection.ProductionBranch, binding.SettingsErr)
		}
		return
	}

	on := theme.On(w)
	fmt.Fprintln(w)
	if !settings.AutoDeployEnabled {
		Warning(w, "Auto-deploy is off, so a push to %s will not deploy anything.", connection.ProductionBranch)
		nextStep(w, on, binding, "Turn it on with ", "auto-deploy", "on")
		return
	}

	targets := deployTargets(settings)
	if len(targets) == 0 {
		Warning(w, "Auto-deploy is on, but neither functions nor a frontend is selected, so a push deploys nothing.")
		nextStep(w, on, binding, "Deploy functions with ", "functions", "on")
	} else {
		fmt.Fprintf(w, "%s%s\n",
			theme.Dim(fmt.Sprintf("A push to %s deploys: ", connection.ProductionBranch), on),
			strings.Join(targets, ", "),
		)
	}
	if frontendTarget(settings) == "" {
		nextStep(w, on, binding, "To deploy a frontend on push too, run ", "frontend", "<name> --root <path>")
	}
}

// nextStep names the git subcommand that changes what a push deploys, when the
// caller gave the command group's path to name it with and did not just run it.
func nextStep(w io.Writer, on bool, binding GitBinding, lead, subcommand, args string) {
	if binding.GitCommand == "" || binding.Changed == subcommand {
		return
	}
	fmt.Fprintf(w, "%s%s\n", theme.Dim(lead, on), theme.Command(binding.GitCommand+" "+subcommand+" "+args, on))
}

func deployTargets(settings *apiclient.ProjectGitDeploySettings) []string {
	targets := make([]string, 0, 2)
	if settings.DeployFunctions {
		targets = append(targets, "functions")
	}
	if frontend := frontendTarget(settings); frontend != "" {
		targets = append(targets, "frontend "+frontend)
	}
	return targets
}

// frontendTarget names the frontend a push deploys and, when it builds from a
// subdirectory, that directory: "web (apps/web)". It is empty when a push
// deploys no frontend.
func frontendTarget(settings *apiclient.ProjectGitDeploySettings) string {
	if settings == nil || settings.FrontendName == nil || strings.TrimSpace(*settings.FrontendName) == "" {
		return ""
	}
	frontend := *settings.FrontendName
	if settings.FrontendAppRoot != nil && strings.TrimSpace(*settings.FrontendAppRoot) != "" {
		frontend += " (" + *settings.FrontendAppRoot + ")"
	}
	return frontend
}
