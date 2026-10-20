package project

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/deploylink"
	"github.com/Kong/volcano-cli/internal/output"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/theme"
)

const (
	installPollInterval = 5 * time.Second
	installWaitTimeout  = 30 * time.Minute
)

var commitPrefixPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// createFromOptions are the settings of `projects create --from`.
type createFromOptions struct {
	deps   cliruntime.Deps
	name   string
	link   string
	commit string
	yes    bool
	noWait bool
	in     io.Reader
	out    io.Writer
}

// checkFromFlags refuses settings that only mean something with --from, so a
// misplaced flag fails instead of being ignored.
func checkFromFlags(from, commit string, yes, noWait bool) error {
	if from != "" {
		return nil
	}
	for flag, set := range map[string]bool{"--commit": commit != "", "--yes": yes, "--no-wait": noWait} {
		if set {
			return fmt.Errorf("%s only applies with --from", flag)
		}
	}
	return nil
}

// runCreateFrom creates a project from a deploy link: it shows what the link
// deploys, gets the person's confirmation of that exact commit, creates the
// project pinned to it, and waits for the installation.
func runCreateFrom(ctx context.Context, opts createFromOptions) error {
	linkID, err := deploylink.ParseLink(opts.link)
	if err != nil {
		return err
	}
	if opts.commit != "" {
		opts.commit = strings.ToLower(strings.TrimSpace(opts.commit))
		if !commitPrefixPattern.MatchString(opts.commit) {
			return fmt.Errorf("invalid --commit %q: pass the commit's hash, at least 7 hexadecimal characters", opts.commit)
		}
	}

	projects := cliproject.NewService(opts.deps)
	preview, err := projects.PreviewDeployLink(ctx, linkID)
	if err != nil {
		return err
	}

	project, err := confirmAndCreate(ctx, opts, projects, linkID, preview)
	if errors.Is(err, errDeclined) {
		return nil
	}
	if err != nil {
		return err
	}

	output.Success(opts.out, "Project created: %s (%s)", project.Name, project.Id.String())
	if opts.noWait {
		fmt.Fprintf(opts.out, "Installation continues on the server. Check it with: %s\n",
			cliruntime.CommandPath(opts.deps, "projects get "+project.Id.String()))
		return nil
	}
	return waitForInstallation(ctx, opts, projects, project, preview)
}

// errDeclined reports that the person answered no. It ends the command quietly,
// like every other declined prompt.
var errDeclined = errors.New("declined")

// confirmAndCreate shows the preview, asks for the commit to be confirmed, and
// creates the project. It returns errDeclined when the person says no.
//
// When the branch moved after the preview the API creates nothing. Someone at a
// terminal sees the new preview and is asked again. A run that already decided,
// with --yes or --commit, never accepts a commit it did not see: it shows the new
// preview and stops.
func confirmAndCreate(
	ctx context.Context, opts createFromOptions, projects cliproject.Service, linkID string,
	preview *apiclient.DeployLinkPreview,
) (*apiclient.Project, error) {
	for {
		fmt.Fprintln(opts.out)
		output.DeployLinkPreview(opts.out, preview)
		fmt.Fprintln(opts.out)

		if opts.commit != "" && !strings.HasPrefix(preview.CommitSha, opts.commit) {
			return nil, fmt.Errorf("the deploy link is at commit %s, not the %s that --commit expects; nothing was created",
				preview.CommitSha, opts.commit)
		}
		if !opts.yes {
			confirmed, err := confirm.Action(opts.in, opts.out,
				"This creates a project and deploys the code at this exact commit.", "Deploy this commit?")
			if err != nil {
				return nil, err
			}
			if !confirmed {
				return nil, errDeclined
			}
		}

		project, err := projects.CreateFromDeployLink(ctx, opts.name, linkID, preview.CommitSha, preview.Path)
		if err == nil {
			return project, nil
		}
		if !cliproject.IsCommitChanged(err) {
			return nil, err
		}

		// The refusal creates nothing, so showing the new commit is safe either way.
		fmt.Fprintf(opts.out, "\n%s\n", theme.Warn("The repository changed since the preview. Here is what the link deploys now.", theme.On(opts.out)))
		preview, err = projects.PreviewDeployLink(ctx, linkID)
		if err != nil {
			return nil, err
		}
		if opts.yes || opts.commit != "" {
			fmt.Fprintln(opts.out)
			output.DeployLinkPreview(opts.out, preview)
			return nil, errors.New("the repository changed since the preview, so nothing was created and the new commit " +
				"was not deployed. Review the commit above and run the command again")
		}
	}
}

// waitForInstallation polls the project until its template installation is
// ready or failed, printing each phase it reaches.
func waitForInstallation(
	ctx context.Context, opts createFromOptions, projects cliproject.Service,
	project *apiclient.Project, preview *apiclient.DeployLinkPreview,
) error {
	out := opts.out
	on := theme.On(out)
	projectID := project.Id.String()
	getCommand := cliruntime.CommandPath(opts.deps, "projects get "+projectID)

	fmt.Fprintf(out, "\nInstalling %s@%s...\n",
		theme.StripControl(preview.Repository.FullName), output.ShortCommit(preview.CommitSha))

	ticker := cliruntime.NewTicker(opts.deps, installPollInterval)
	defer ticker.Stop()
	timeout := cliruntime.NewTimer(opts.deps, installWaitTimeout)
	defer timeout.Stop()

	var lastPhase apiclient.ProjectTemplateInstallationPhase
	current := project
	for {
		installation := current.TemplateInstallation
		if installation != nil {
			if installation.Phase != lastPhase {
				lastPhase = installation.Phase
				fmt.Fprintf(out, "  - %s\n", installation.Phase)
			}
			switch installation.Status {
			case apiclient.ProjectTemplateInstallationStatusReady:
				printInstalled(opts, current, preview)
				return nil
			case apiclient.ProjectTemplateInstallationStatusFailed:
				fmt.Fprintf(out, "  %s installation failed during %s\n", theme.Fail("✗", on), installation.Phase)
				return fmt.Errorf("installing project %s failed; the project is kept. Inspect it with `%s`, "+
					"or delete it with `%s`", projectID, getCommand,
					cliruntime.CommandPath(opts.deps, "projects delete "+projectID))
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout.C():
			return fmt.Errorf("stopped waiting after %s; the installation continues on the server. Check it with `%s`",
				installWaitTimeout, getCommand)
		case <-ticker.C():
		}

		var err error
		current, err = projects.Get(ctx, projectID)
		if err != nil {
			return fmt.Errorf("%w\nThe installation continues on the server. Check it with `%s`", err, getCommand)
		}
	}
}

func printInstalled(opts createFromOptions, project *apiclient.Project, preview *apiclient.DeployLinkPreview) {
	on := theme.On(opts.out)
	source := theme.StripControl(preview.Repository.FullName) + "@" + output.ShortCommit(preview.CommitSha)
	if installed := project.TemplateInstallation.Source; installed != nil {
		source = output.TemplateSource(installed)
	}
	projectID := project.Id.String()
	fmt.Fprintf(opts.out, "  %s ready\n", theme.Success("✓", on))
	output.Success(opts.out, "Project ready: %s (%s)", project.Name, projectID)
	fmt.Fprintf(opts.out, "%s %s\n", theme.Dim("Source:", on), source)
	fmt.Fprintf(opts.out, "See it: %s\nSelect it: %s\n",
		cliruntime.CommandPath(opts.deps, "projects get "+projectID),
		cliruntime.CommandPath(opts.deps, "use "+projectID))
}
