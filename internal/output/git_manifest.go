package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/theme"
)

// GitManifest is the volcano-config.yaml sections a push applies, with the
// binding and settings that decide whether a push runs at all.
type GitManifest struct {
	// Connection is the project's binding, nil when nothing is connected.
	Connection *apiclient.ProjectGitConnection
	Settings   *apiclient.ProjectGitDeploySettings
	Project    string
	// GitCommand is the path of the git command group, such as "volcano git".
	GitCommand string
	// ConfigDeployCommand applies what a push leaves out, such as
	// "volcano cloud config deploy".
	ConfigDeployCommand string
}

// GitManifestSections renders the sections a push applies, as a report.
func GitManifestSections(w io.Writer, manifest GitManifest) {
	gitManifestDetail(w, manifest)
	gitManifestOutcome(w, manifest, true)
}

// GitManifestSectionsSet renders the sections a push applies from now on.
// Nothing is applied by choosing them, which is worth saying: the next push
// does that.
func GitManifestSectionsSet(w io.Writer, manifest GitManifest) {
	if sections := manifest.Settings.ManifestSections; len(sections) > 0 {
		Success(w, "A push now applies %s from volcano-config.yaml", manifestSectionList(sections))
	} else {
		Success(w, "A push now applies no volcano-config.yaml sections")
	}
	gitManifestDetail(w, manifest)
	Note(w, "Nothing was applied.")
	gitManifestOutcome(w, manifest, false)
}

func gitManifestDetail(w io.Writer, manifest GitManifest) {
	on := theme.On(w)
	if manifest.Project != "" {
		kv(w, on, "Project", "%s", manifest.Project)
	}
	if connection := manifest.Connection; connection != nil {
		kv(w, on, "Repository", "%s", connection.RepoFullName)
		kv(w, on, "Production branch", "%s (%s)", connection.ProductionBranch, branchSource(connection))
	}
	sections := "none"
	if chosen := manifest.Settings.ManifestSections; len(chosen) > 0 {
		sections = manifestSectionList(chosen)
	}
	kv(w, on, "Manifest sections", "%s", sections)
}

// gitManifestOutcome says what the next push does with the sections, and the
// one command that changes the answer when a push applies nothing. suggestSet
// is false right after the sections were set, which would only repeat it.
func gitManifestOutcome(w io.Writer, manifest GitManifest, suggestSet bool) {
	on := theme.On(w)
	connection, settings := manifest.Connection, manifest.Settings
	fmt.Fprintln(w)
	switch {
	case len(settings.ManifestSections) == 0:
		fmt.Fprintln(w, theme.Dim("A push applies none of volcano-config.yaml.", on))
		if suggestSet && manifest.GitCommand != "" {
			fmt.Fprintf(w, "%s%s\n", theme.Dim("To apply its frontends on push, run ", on),
				theme.Command(manifest.GitCommand+" manifest-sections set frontends", on))
		}
		return
	case connection == nil:
		Warning(w, "No repository is connected, so no push applies them yet.")
		if manifest.GitCommand != "" {
			fmt.Fprintf(w, "%s%s\n", theme.Dim("Connect one with ", on), theme.Command(manifest.GitCommand+" connect", on))
		}
		return
	case !settings.AutoDeployEnabled:
		Warning(w, "Auto-deploy is off, so a push to %s applies nothing.", connection.ProductionBranch)
		if manifest.GitCommand != "" {
			fmt.Fprintf(w, "%s%s\n", theme.Dim("Turn it on with ", on), theme.Command(manifest.GitCommand+" auto-deploy on", on))
		}
		return
	}
	fmt.Fprintf(w, "%s\n", theme.Dim(fmt.Sprintf("A push to %s applies these once its deploys succeed. "+
		"It only adds: deletes, replacements and wider exposure are left for %s.",
		connection.ProductionBranch, manifest.ConfigDeployCommand), on))
}

func manifestSectionList(sections []apiclient.ProjectGitManifestSection) string {
	names := make([]string, len(sections))
	for i, section := range sections {
		names[i] = clean(string(section))
	}
	return strings.Join(names, ", ")
}
