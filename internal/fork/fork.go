// Package fork creates a person's own copy of a shared project: a cloud
// project installed from the release a share link offers. volcano fork and
// volcano projects create --from both run it.
//
// The copy needs no variable values. Volcano creates every variable the release
// declares, empty unless a value is given, so a fork never stops to ask for
// keys; the person sets them afterwards.
package fork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sharing"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	installPollInterval = 5 * time.Second
	installWaitTimeout  = 30 * time.Minute

	// fallbackName names a copy whose link suggests no name, as the web does.
	fallbackName = "deployed-project"
)

// Options are the settings of one fork.
type Options struct {
	Deps cliruntime.Deps
	// Name is the copy's project name. Empty takes the name the link suggests.
	Name string
	// Link is the share link: its URL or its 12-character id.
	Link string
	// Vars are NAME=value flags for variables to fill in up front.
	Vars   []string
	Yes    bool
	NoWait bool
	// JSON prints the created project as JSON on Stdout once it is done.
	JSON bool
	// Select makes the copy the active project once it is created.
	Select bool
	In     io.Reader
	// Out receives the preview, prompts and progress.
	Out io.Writer
	// Stdout receives the JSON result.
	Stdout io.Writer
}

// errDeclined reports that the person answered no. It ends the command quietly,
// like every other declined prompt.
var errDeclined = errors.New("declined")

// Run creates a copy of the project a share link offers: it shows the release,
// gets the person's confirmation of that exact release, creates the project,
// and waits for the installation.
func Run(ctx context.Context, opts Options) error {
	shareID, err := sharing.ParseLink(opts.Link)
	if err != nil {
		return err
	}
	given, err := ParseVarFlags(opts.Vars)
	if err != nil {
		return err
	}
	ask := !opts.Yes && confirm.CanPrompt(opts.In) && !confirm.MarkedNonInteractive()
	if !opts.Yes && !ask {
		return errors.New("forking a shared project needs your confirmation, and nothing can answer a prompt here: " +
			"review the release at a terminal, or pass --yes")
	}

	projects := cliproject.NewService(opts.Deps)
	preview, err := projects.PreviewShare(ctx, shareID)
	if err != nil {
		return err
	}
	if opts.Name == "" {
		opts.Name = defaultName(preview.SuggestedName)
	}

	creation := creation{
		opts:     opts,
		projects: projects,
		shareID:  shareID,
		given:    given,
		ask:      ask,
		// One key per confirmation: a retry of the same request then answers
		// with the project the first attempt created.
		idempotencyKey: uuid.NewString(),
	}
	project, preview, err := creation.confirmAndCreate(ctx, preview)
	if errors.Is(err, errDeclined) {
		return nil
	}
	if err != nil {
		return err
	}

	output.Success(opts.Out, "Project created: %s (%s)", project.Name, project.Id.String())
	if project.TemplateInstallation == nil {
		return notInstalling(opts, project)
	}
	if opts.NoWait {
		fmt.Fprintf(opts.Out, "Installation continues on the server. Check it with: %s\n",
			cliruntime.CommandPath(opts.Deps, "projects get "+project.Id.String()))
		return finish(ctx, opts, project, preview)
	}
	installed, err := waitForInstallation(ctx, opts, projects, project, preview)
	if err != nil {
		return err
	}
	return finish(ctx, opts, installed, preview)
}

// finish selects the copy when asked, says which variables it starts with
// empty and how to set them on the copy, and prints the JSON result.
func finish(ctx context.Context, opts Options, project *apiclient.Project, preview *apiclient.SharePreview) error {
	selected, err := selectCopy(ctx, opts, project)
	emptyVariables(opts, project, preview, selected)
	if err != nil {
		return err
	}
	return printJSON(opts, project)
}

// selectCopy makes the copy the active project when asked, unless the working
// directory is linked to another project. It reports whether the copy is now
// the project cloud commands use.
func selectCopy(ctx context.Context, opts Options, project *apiclient.Project) (bool, error) {
	if !opts.Select {
		return false, nil
	}
	if link, err := config.FindLinkFromWorkingDir(); err == nil && link != nil {
		// Selecting here would relink the directory to the copy, and the
		// directory's own project would then deploy to it.
		fmt.Fprintf(opts.Out, "This directory stays linked to %s. Select the copy elsewhere with `%s`.\n",
			projectName(link.Project), cliruntime.CommandPath(cliruntime.Deps{}, "use "+project.Id.String()))
		return false, nil
	}
	result, err := cliproject.NewService(opts.Deps).Use(ctx, project.Id.String())
	if err != nil {
		return false, fmt.Errorf("the copy is created, but it could not be made the active project: %w. Select it with `%s`",
			err, cliruntime.CommandPath(cliruntime.Deps{}, "use "+project.Id.String()))
	}
	output.Success(opts.Out, "Now using project: %s (%s)", result.Project.Name, result.Project.Id.String())
	if result.EnvErr != nil {
		output.Warning(opts.Out, "Could not update the anon key in %s for this project: %v.", result.EnvFile, result.EnvErr)
	}
	// VOLCANO_PROJECT_ID outranks the project use saves.
	if cfg, err := config.Load(); err == nil {
		if selected := cfg.SelectedProjectID(); selected != "" && !isProject(selected, project.Id) {
			output.Warning(opts.Out, "VOLCANO_PROJECT_ID selects project %s, so cloud commands still use it. "+
				"Unset it to use the copy.", theme.StripControl(selected))
			return false, nil
		}
	}
	return true, nil
}

// isProject reports whether id, in any UUID spelling, names project.
func isProject(id string, project uuid.UUID) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed == project
}

// emptyVariables lists the variables the copy starts with empty. cloud
// variables deploy writes to the active project, so when the copy is not it,
// the hint selects the copy first: following it must not change another
// project's variables.
func emptyVariables(opts Options, project *apiclient.Project, preview *apiclient.SharePreview, selected bool) {
	var empty []string
	for _, variable := range preview.Release.Variables {
		if value, ok := ParseVarValue(opts.Vars, variable.Name); !ok || value == "" {
			empty = append(empty, variable.Name)
		}
	}
	deploy := cliruntime.CommandPath(cliruntime.Deps{}, "cloud variables deploy --file <env-file>")
	hint := fmt.Sprintf("Set them in an env file and run `%s`, or in the dashboard.", deploy)
	if !selected {
		hint = fmt.Sprintf("Set them in the dashboard, or select the copy with `%s` outside a linked directory, "+
			"then set them in an env file and run `%s`.",
			cliruntime.CommandPath(cliruntime.Deps{}, "use "+project.Id.String()), deploy)
	}
	output.EmptyVariables(opts.Out, empty, hint)
}

func printJSON(opts Options, project *apiclient.Project) error {
	if !opts.JSON {
		return nil
	}
	encoder := json.NewEncoder(opts.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(project)
}

func projectName(project config.ProjectConfig) string {
	if project.Name != "" {
		return theme.StripControl(project.Name)
	}
	return theme.StripControl(project.ID)
}

var nameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// defaultName is the copy's name when none is given: the name the link
// suggests, made safe for a project name, else a generic one. Volcano adds a
// short suffix to a name the person already uses.
func defaultName(suggested string) string {
	name := strings.Trim(nameUnsafe.ReplaceAllString(suggested, "-"), "-")
	if len(name) > 255 {
		name = strings.TrimRight(name[:255], "-")
	}
	if name == "" {
		return fallbackName
	}
	return name
}

// notInstalling reports a project the API created without an installation:
// nothing is being installed, so there is nothing to wait for.
func notInstalling(opts Options, project *apiclient.Project) error {
	projectID := project.Id.String()
	return fmt.Errorf("project %s was created, but the release is not being installed in it, so treat it as not installed. "+
		"Inspect it with `%s`, or delete it with `%s`", projectID,
		cliruntime.CommandPath(opts.Deps, "projects get "+projectID),
		cliruntime.CommandPath(opts.Deps, "projects delete "+projectID))
}

// ParseVarFlags reads --var NAME=value flags. A value may be empty, which is a
// value, and may itself contain '='.
func ParseVarFlags(flags []string) (map[string]string, error) {
	values := make(map[string]string, len(flags))
	for _, flag := range flags {
		name, value, ok := strings.Cut(flag, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			// The flag may hold a secret, so the error never repeats it.
			return nil, errors.New("invalid --var: each one must be NAME=value, with the variable's name before '='")
		}
		if _, seen := values[name]; seen {
			return nil, fmt.Errorf("--var %s is given more than once", name)
		}
		values[name] = value
	}
	return values, nil
}

// ParseVarValue returns the value a --var flag gives a variable.
func ParseVarValue(flags []string, name string) (string, bool) {
	values, err := ParseVarFlags(flags)
	if err != nil {
		return "", false
	}
	value, ok := values[name]
	return value, ok
}

// creation is one run's confirmation and create, which may go around more than
// once when the share moves to a new release in between.
type creation struct {
	opts           Options
	projects       cliproject.Service
	shareID        string
	given          map[string]string
	ask            bool
	idempotencyKey string
}

// confirmAndCreate shows the preview, asks for the release to be confirmed,
// and creates the project. It returns errDeclined when the person says no, and
// the preview of the release it created from.
//
// When the share has a new release after the preview the API creates nothing.
// Someone at a terminal sees what changed and is asked again. A run that
// already decided, with --yes or without a terminal, never installs a release
// it did not see: it shows the new release and stops.
func (c creation) confirmAndCreate(
	ctx context.Context, preview *apiclient.SharePreview,
) (*apiclient.Project, *apiclient.SharePreview, error) {
	out := c.opts.Out
	fmt.Fprintln(out)
	output.SharePreview(out, preview)
	for {
		values, err := c.values(preview.Release.Variables)
		if err != nil {
			return nil, nil, err
		}
		if c.ask {
			fmt.Fprintln(out)
			confirmed, err := confirm.Action(c.opts.In, out,
				fmt.Sprintf("This creates the project %s in your account and deploys this release in it.", c.opts.Name),
				"Fork it?")
			if err != nil {
				return nil, nil, err
			}
			if !confirmed {
				return nil, nil, errDeclined
			}
		}

		project, err := c.projects.Fork(ctx, api.ForkRequest{
			Name:           c.opts.Name,
			ShareID:        c.shareID,
			ReleaseID:      preview.Release.Id,
			Variables:      values,
			IdempotencyKey: c.idempotencyKey,
		})
		if err == nil {
			return project, preview, nil
		}
		if !cliproject.IsReleaseChanged(err) {
			return nil, nil, err
		}

		// The refusal creates nothing, so showing the new release is safe either way.
		previous := preview.Release
		preview, err = c.projects.PreviewShare(ctx, c.shareID)
		if err != nil {
			return nil, nil, err
		}
		fmt.Fprintf(out, "\n%s\n", theme.Warn("The project's owner published a new release since the preview. What changed:", theme.On(out)))
		output.ShareReleaseChanges(out, &previous, &preview.Release)
		fmt.Fprintln(out)
		output.SharePreview(out, preview)
		if !c.ask {
			return nil, nil, errors.New("the shared project has a new release since the preview, so nothing was created " +
				"and the new release was not deployed. Review the release above and run the command again")
		}
		// Confirming the new release is a new request, so it gets its own key.
		c.idempotencyKey = uuid.NewString()
	}
}

// values returns the --var values for variables the release declares. A --var
// the release does not declare is refused; every variable left out starts
// empty in the copy.
func (c creation) values(variables []apiclient.ShareVariable) (map[string]string, error) {
	listed := make([]string, len(variables))
	for i, variable := range variables {
		listed[i] = variable.Name
	}
	values := map[string]string{}
	for name, value := range c.given {
		if !slices.Contains(listed, name) {
			return nil, fmt.Errorf("--var %s: the release does not declare this variable; it declares: %s",
				name, strings.Join(nonEmpty(listed), ", "))
		}
		values[name] = value
	}
	return values, nil
}

func nonEmpty(names []string) []string {
	if len(names) == 0 {
		return []string{"none"}
	}
	return names
}

// waitForInstallation polls the project until its installation is ready or
// failed, printing each phase it reaches, and returns the ready project.
func waitForInstallation(
	ctx context.Context, opts Options, projects cliproject.Service,
	project *apiclient.Project, preview *apiclient.SharePreview,
) (*apiclient.Project, error) {
	out := opts.Out
	on := theme.On(out)
	projectID := project.Id.String()
	getCommand := cliruntime.CommandPath(opts.Deps, "projects get "+projectID)

	fmt.Fprintf(out, "\nInstalling release %s...\n", preview.Release.Id.String())

	ticker := cliruntime.NewTicker(opts.Deps, installPollInterval)
	defer ticker.Stop()
	timeout := cliruntime.NewTimer(opts.Deps, installWaitTimeout)
	defer timeout.Stop()

	var lastPhase apiclient.ProjectTemplateInstallationPhase
	current := project
	for {
		installation := current.TemplateInstallation
		if installation == nil {
			return nil, notInstalling(opts, current)
		}
		if installation.Phase != lastPhase {
			lastPhase = installation.Phase
			fmt.Fprintf(out, "  - %s\n", installation.Phase)
		}
		switch installation.Status {
		case apiclient.ProjectTemplateInstallationStatusReady:
			printInstalled(opts, current, preview)
			return current, nil
		case apiclient.ProjectTemplateInstallationStatusFailed:
			reason := output.InstallationFailure(installation.FailureCode)
			fmt.Fprintf(out, "  %s installation failed during %s: %s\n", theme.Fail("✗", on), installation.Phase, reason)
			return nil, fmt.Errorf("installing project %s failed: %s. The project is kept. Inspect it with `%s`, "+
				"or delete it with `%s`", projectID, reason, getCommand,
				cliruntime.CommandPath(opts.Deps, "projects delete "+projectID))
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C():
			return nil, fmt.Errorf("stopped waiting after %s; the installation continues on the server. Check it with `%s`",
				installWaitTimeout, getCommand)
		case <-ticker.C():
		}

		var err error
		current, err = projects.Get(ctx, projectID)
		if err != nil {
			return nil, fmt.Errorf("%w\nThe installation continues on the server. Check it with `%s`", err, getCommand)
		}
	}
}

func printInstalled(opts Options, project *apiclient.Project, preview *apiclient.SharePreview) {
	on := theme.On(opts.Out)
	source := output.TemplateSource(&apiclient.ProjectTemplateInstallationSource{
		Kind: apiclient.ProjectTemplateInstallationSourceKindShareRelease, ShareId: preview.Id, ReleaseId: preview.Release.Id,
	})
	if installed := project.TemplateInstallation.Source; installed != nil {
		source = output.TemplateSource(installed)
	}
	projectID := project.Id.String()
	fmt.Fprintf(opts.Out, "  %s ready\n", theme.Success("✓", on))
	output.Success(opts.Out, "Project ready: %s (%s)", project.Name, projectID)
	fmt.Fprintf(opts.Out, "%s %s\n", theme.Dim("Source:", on), source)
	if !opts.Select {
		fmt.Fprintf(opts.Out, "See it: %s\nSelect it: %s\n",
			cliruntime.CommandPath(opts.Deps, "projects get "+projectID),
			cliruntime.CommandPath(cliruntime.Deps{}, "use "+projectID))
	}
}
