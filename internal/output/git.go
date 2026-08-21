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
}

// GitConnected renders a repository binding that was just made, followed by
// what a push to the production branch will actually deploy. Settings are
// optional: a connect still succeeded if reading them afterwards did not.
func GitConnected(w io.Writer, binding GitBinding) {
	Success(w, "Connected %s", binding.Connection.RepoFullName)
	gitBindingDetail(w, theme.On(w), binding)
	gitDeploySettings(w, binding)
}

// GitCreation is a repository this CLI created, and what was done to the
// checkout afterwards. The local half is reported rather than assumed: the push
// is skipped in a case the user has to act on, and a report that implied it
// happened would hide the one failure that produces no error anywhere.
type GitCreation struct {
	Binding GitBinding
	// AppInstalled is false when Volcano could not confirm the GitHub App can see
	// the new repository. The binding is complete either way; no push deploys
	// until access is granted.
	AppInstalled bool
	// InstallURL is where that access is granted, when it is missing.
	InstallURL  *string
	RemoteAdded bool
	RemoteName  string
	Pushed      bool
	// NextCommands is what the user has to run themselves, for every case where
	// this command did not push. It starts from whatever was not already done, so
	// a checkout that got its remote is told only to push.
	NextCommands []string
	// Routing describes where this checkout's configuration sends a later bare
	// `git push`.
	Routing GitPushRouting
}

// GitPushRouting is what a bare `git push` in this checkout would do after the
// repository was created.
//
// Worth reporting because `--set-upstream` loses to it: git consults
// branch.<name>.pushRemote and remote.pushDefault before the upstream, so a
// checkout with either set keeps pushing to the old destination. The new
// repository then stops receiving commits and the project stops deploying, with
// nothing failing anywhere.
type GitPushRouting struct {
	Elsewhere bool
	Target    string
	Source    string
	Err       error
	// Command is the push that does reach the new repository.
	Command string
}

// GitCreated renders a created repository: what exists on GitHub now, what the
// project is bound to, and what is left for the user to do.
func GitCreated(w io.Writer, creation GitCreation) {
	on := theme.On(w)
	connection := creation.Binding.Connection
	Success(w, "Created %s", connection.RepoFullName)
	gitBindingDetail(w, on, creation.Binding)
	if creation.RemoteAdded {
		kv(w, on, "Remote", "%s", creation.RemoteName)
	}
	if creation.Pushed {
		Success(w, "Pushed %s to %s", connection.ProductionBranch, connection.RepoFullName)
	}
	// Only when a push can deploy. Said above the access warning it read as a flat
	// contradiction of it — one line promising a push deploys functions, the next
	// saying a push deploys nothing — so in that case it moves below, reworded.
	if creation.AppInstalled {
		gitDeploySettings(w, creation.Binding)
	}
	gitCreationNextStep(w, on, creation)
}

// gitCreationNextStep says what is left. Access to grant comes first: a push
// without it lands the code and deploys nothing, silently, so it is the one
// thing worth interrupting for.
func gitCreationNextStep(w io.Writer, on bool, creation GitCreation) {
	if !creation.AppInstalled {
		fmt.Fprintln(w)
		Warning(w, "The Volcano GitHub App could not be confirmed to have access to %s, "+
			"so a push would deploy nothing.", creation.Binding.Connection.RepoFullName)
		if creation.InstallURL != nil && *creation.InstallURL != "" {
			fmt.Fprintf(w, "%s\n  %s\n", theme.Dim("Grant it access here:", on), *creation.InstallURL)
		}
		gitCreationDeferredDeploy(w, on, creation.Binding)
		gitCreationCommands(w, on, "Then push:", creation.NextCommands)
		return
	}
	if creation.Pushed {
		gitPushRouting(w, on, creation.Routing)
		return
	}
	gitCreationCommands(w, on, "Push to deploy:", creation.NextCommands)
}

// gitPushRouting warns when the push that just succeeded is the last one this
// checkout will send to the new repository on its own.
func gitPushRouting(w io.Writer, on bool, routing GitPushRouting) {
	switch {
	case routing.Err != nil:
		fmt.Fprintln(w)
		Warning(w, "This checkout's push configuration could not be read, so whether a later "+
			"%s reaches the new repository is unknown: %v", "git push", routing.Err)
	case routing.Elsewhere:
		fmt.Fprintln(w)
		Warning(w, "%s sends this branch's pushes to %s, which outranks the upstream just set — "+
			"so a bare %s will not reach the new repository.", routing.Source, routing.Target, "git push")
		fmt.Fprintf(w, "%s%s\n", theme.Dim("Push to it with: ", on), theme.Command(routing.Command, on))
	}
}

// gitCreationDeferredDeploy says what a push will deploy once the App can see
// the repository — the same fact gitDeploySettings reports, phrased for a state
// where it is not true yet. A failed settings read is still surfaced: "nothing
// configured" and "could not find out" are different answers, and this is the
// case where the user is already being asked to go and fix something.
func gitCreationDeferredDeploy(w io.Writer, on bool, binding GitBinding) {
	settings := binding.Settings
	if settings == nil {
		if binding.SettingsErr != nil {
			Warning(w, "Its deploy settings could not be read either, so what a push to %s "+
				"will deploy is unknown: %v", binding.Connection.ProductionBranch, binding.SettingsErr)
		}
		return
	}
	targets := deployTargets(settings)
	if !settings.AutoDeployEnabled || len(targets) == 0 {
		// Nothing would deploy even with access, which the warning above already
		// covers; adding "and nothing is selected" here only dilutes it.
		return
	}
	fmt.Fprintf(w, "%s%s\n",
		theme.Dim(fmt.Sprintf("Once it can, a push to %s deploys: ", binding.Connection.ProductionBranch), on),
		strings.Join(targets, ", "),
	)
}

func gitCreationCommands(w io.Writer, on bool, instruction string, commands []string) {
	if len(commands) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n", theme.Dim(instruction, on))
	for _, command := range commands {
		fmt.Fprintf(w, "  %s\n", theme.Command(command, on))
	}
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
// behind it does not do.
func GitStatus(w io.Writer, binding GitBinding) {
	gitBindingDetail(w, theme.On(w), binding)
	gitDeploySettings(w, binding)
}

// GitNotConnected renders a project with no repository bound. This is a state,
// not a failure, so it reads as an answer and says what would change it.
func GitNotConnected(w io.Writer, project, connectCommand string) {
	on := theme.On(w)
	kv(w, on, "Project", "%s", project)
	fmt.Fprintf(w, "%s\n", theme.Dim("No repository is connected, so pushes do not deploy.", on))
	fmt.Fprintf(w, "\n%s%s\n", theme.Dim("Connect one with ", on), theme.Command(connectCommand, on))
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
	kv(w, on, "Production branch", "%s", connection.ProductionBranch)
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
