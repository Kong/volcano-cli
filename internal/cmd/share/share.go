// Package share wires volcano share: sharing the current cloud project so
// anyone can fork their own copy of it.
package share

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	connectcmd "github.com/Kong/volcano-cli/internal/cmd/connect"
	deploycmd "github.com/Kong/volcano-cli/internal/cmd/deploy"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/sharing"
	"github.com/Kong/volcano-cli/internal/theme"
)

const maxDescriptionLength = 280

type options struct {
	deps         cliruntime.Deps
	linkID       string
	name         string
	description  *string
	allowReshare *bool
	envFile      string
	noDeploy     bool
	yes          bool
	jsonOutput   bool
	in           io.Reader
	// out receives the summary, prompts and progress; stdout the result.
	out    io.Writer
	stdout io.Writer
	errOut io.Writer

	// The steps that put the working directory on Volcano Cloud. They run the
	// connect and cloud deploy commands; tests replace them.
	connect func(ctx context.Context, opts options) error
	deploy  func(ctx context.Context, opts options) error
	// directory says what the working directory is: a Volcano project linked
	// to a cloud project, one that is not, or neither.
	directory func() directory
	// deploysFromGit reports a cloud project whose source is its Git
	// repository, which refuses direct deploys.
	deploysFromGit func(ctx context.Context, opts options) bool
}

// directory is what the working directory holds.
type directory struct {
	// path is the project directory, empty outside one.
	path string
	// link is the cloud project the directory is linked to, nil when none.
	link *config.Link
}

func (d directory) project() bool { return d.path != "" }

// New returns the share command.
func New(deps cliruntime.Deps) *cobra.Command {
	opts := options{}
	var description string
	var allowReshare, noAllowReshare bool
	cmd := &cobra.Command{
		Use:   "share",
		Short: "Share the current cloud project so anyone can fork their own copy",
		Long: `Share the current cloud project: anyone with its link can fork their own copy
of it into their Volcano account with 'volcano fork <link>', or from the link's
page.

In a Volcano project directory, volcano share shares what is in the directory:
it deploys the directory to its linked cloud project, as 'volcano cloud deploy'
does, waits for the deploy, and then publishes it. A directory that is not on
Volcano Cloud yet first gets a cloud project, linked as 'volcano connect .'
links it. That deploy sends no env file, because volcano/volcano.env holds the
local server's values: variables the project declares start empty. Pass
--env-file to deploy your own values to your cloud project; the share still
carries only their names. --no-deploy deploys nothing and creates no cloud
project: it shares what the linked or selected cloud project already runs. A project that deploys from its Git repository is
never deployed directly: the command shares what it runs. Outside a project
directory, the command shares the selected cloud project as it runs.

Sharing publishes a release Volcano stores, and prints the link and a deploy
button for a README. Run it again to share what the project has then. A share
carries names only: no variable values, keys, sign-in settings or secret files
such as .env. Every copy gets the project's variables empty, for its owner to
set. Functions that read VOLCANO_SERVICE_KEY get each copy's own service key,
which bypasses row-level security, so the command asks you to confirm them.

--description says what the project does, on the link's page. Copies cannot be
shared again unless --allow-reshare lets them; --no-allow-reshare takes that
back, and stops copies that are already shared. --yes confirms without asking,
for CI.`,
		Example: fmt.Sprintf("  %s\n  %s\n  %s",
			cliruntime.CommandPath(deps, "share"),
			cliruntime.CommandPath(deps, `share --description "Ask a question, get a checked answer."`),
			cliruntime.CommandPath(deps, "share --yes --json")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("description") {
				value, err := checkDescription(description)
				if err != nil {
					return err
				}
				opts.description = &value
			}
			switch {
			case allowReshare:
				opts.allowReshare = new(true)
			case noAllowReshare:
				opts.allowReshare = new(false)
			}
			opts.deps = deps
			opts.linkID = strings.TrimSpace(opts.linkID)
			opts.envFile = strings.TrimSpace(opts.envFile)
			opts.in = cmd.InOrStdin()
			opts.stdout = cmd.OutOrStdout()
			opts.out = cmd.OutOrStdout()
			opts.errOut = cmd.ErrOrStderr()
			if opts.jsonOutput {
				opts.out = cmd.ErrOrStderr()
			}
			opts.connect = runConnect
			opts.deploy = runCloudDeploy
			opts.directory = workingDirectory
			opts.deploysFromGit = deploysFromGit
			return run(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "What the project does, shown on the link's page (one line, up to 280 characters)")
	cmd.Flags().BoolVar(&allowReshare, "allow-reshare", false, "Let people share their copies again")
	cmd.Flags().BoolVar(&noAllowReshare, "no-allow-reshare", false, "Stop copies from being shared again")
	cmd.Flags().StringVar(&opts.name, "name", "", "Project name offered to people who fork it, when the share is new (default: none)")
	cmd.Flags().StringVar(&opts.envFile, "env-file", "", "Env file to deploy to your cloud project with the directory (default: none)")
	cmd.Flags().BoolVar(&opts.noDeploy, "no-deploy", false, "Share what the cloud project runs now, without deploying this directory")
	cmd.Flags().StringVar(&opts.linkID, "link", "", "Share to publish to, by its link or id, when the project has several (default: its newest)")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Confirm without asking, including the service key functions")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print the result as JSON")
	cmd.MarkFlagsMutuallyExclusive("allow-reshare", "no-allow-reshare")
	cmd.MarkFlagsMutuallyExclusive("no-deploy", "env-file")
	cmd.AddCommand(newStatus(deps))
	cmd.AddCommand(newOff(deps))
	cmd.AddCommand(newReleases(deps))
	return cmd
}

// errDeclined reports that the person answered no. Nothing more was changed.
var errDeclined = errors.New("declined")

// result is what volcano share printed, as JSON under --json.
type result struct {
	Share    *apiclient.Share        `json:"share"`
	Release  *apiclient.ShareRelease `json:"release"`
	URL      string                  `json:"url"`
	Markdown string                  `json:"markdown"`
	HTML     string                  `json:"html"`
}

func run(ctx context.Context, opts options) error {
	ask := !opts.yes && confirm.CanPrompt(opts.in) && !confirm.MarkedNonInteractive()
	if !opts.yes && !ask {
		return errors.New("sharing a project needs your confirmation, and nothing can answer a prompt here: " +
			"run it at a terminal, or pass --yes")
	}
	err := putDirectoryOnCloud(ctx, opts, opts.directory(), ask)
	if errors.Is(err, errDeclined) {
		return nil
	}
	if err != nil {
		return err
	}

	service := sharing.NewService(opts.deps)
	share, created, err := prepareShare(ctx, opts, service)
	if err != nil {
		return err
	}
	draft, err := service.Draft(ctx, share.Id)
	if err != nil {
		return err
	}
	release, err := publish(ctx, opts, service, share.Id, draft, ask)
	if errors.Is(err, errDeclined) {
		return nil
	}
	if err != nil {
		return err
	}
	share, err = finishShare(ctx, opts, service, share, created)
	if err != nil {
		return err
	}
	return printShared(opts, share, release)
}

// prepareShare returns the share to publish to: the one --link names, else
// the project's newest switched-on share, else its newest, else a new one,
// created with --description and --allow-reshare. created reports a new share.
func prepareShare(ctx context.Context, opts options, service sharing.Service) (*apiclient.Share, bool, error) {
	share, err := pickShare(ctx, service, opts.linkID)
	if err != nil || share != nil {
		return share, false, err
	}
	share, err = service.Create(ctx, apiclient.ShareCreateRequest{
		SuggestedName: sharing.Optional(opts.name),
		Description:   opts.description,
		AllowReshare:  opts.allowReshare,
	})
	return share, err == nil, err
}

// finishShare switches the share on and applies --description and
// --allow-reshare to a share that existed, once the release is published. A
// declined or failed publish therefore changes nothing on the live share, and
// a failed publish never switches an old release back on.
func finishShare(
	ctx context.Context, opts options, service sharing.Service, share *apiclient.Share, created bool,
) (*apiclient.Share, error) {
	update := apiclient.ShareUpdateRequest{}
	if !created {
		update.Description, update.AllowReshare = opts.description, opts.allowReshare
	}
	switchOn := share.Status != apiclient.ShareStatusActive
	if switchOn {
		status := apiclient.ShareUpdateRequestStatusActive
		update.Status = &status
	}
	if update.Status == nil && update.Description == nil && update.AllowReshare == nil {
		if refreshed, err := service.Get(ctx, share.Id); err == nil {
			return refreshed, nil
		}
		return share, nil
	}
	updated, err := service.Update(ctx, share.Id, update)
	if err == nil {
		return updated, nil
	}
	if switchOn {
		return nil, fmt.Errorf("%w\n\nThe new release is published, but sharing is still off. Run the command again to switch it on", err)
	}
	return nil, fmt.Errorf("%w\n\nThe new release is published and shared, but its description and re-sharing setting "+
		"did not change. Run the command again to change them", err)
}

// publish shows what sharing publishes, has the owner confirm it and the
// service key functions, and publishes it with every variable the draft lists.
// When the project changes in between and the fresh build asks for more than
// the owner confirmed, nothing is published: at a terminal the owner sees the
// fresh build and is asked again; with --yes the command stops.
func publish(
	ctx context.Context, opts options, service sharing.Service, shareID string,
	draft *apiclient.ShareReleaseDraft, ask bool,
) (*apiclient.ShareRelease, error) {
	on := theme.On(opts.out)
	for {
		fmt.Fprintf(opts.out, "\n%s\n", theme.Title("This shares", on))
		output.ShareReleaseDraft(opts.out, draft)
		if err := confirmShare(opts, draft, ask); err != nil {
			return nil, err
		}
		release, err := publishConfirmed(ctx, opts, service, shareID, draft)
		if err == nil {
			return release, nil
		}
		changed, ok := errors.AsType[*api.DraftChangedError](err)
		if !ok {
			return nil, err
		}
		reason := theme.StripControl(changed.Err.Message)
		fmt.Fprintf(opts.out, "\n%s\n", theme.Warn("Nothing was shared: the project changed: "+reason, on))
		draft = &changed.Draft
		if !ask {
			fmt.Fprintln(opts.out)
			output.ShareReleaseDraft(opts.out, draft)
			return nil, fmt.Errorf("nothing was shared, because the project changed while it was being shared: %s. "+
				"Review what it would share now, above, and run the command again", reason)
		}
	}
}

// publishConfirmed publishes the draft the owner confirmed. A fresh build that
// is not that draft, because the project was redeployed or changed since, is
// refused with the fresh draft. When that draft has no variable or service key
// function the owner did not already confirm, it is published once, without
// asking again; otherwise the refusal is returned for the owner to review.
func publishConfirmed(
	ctx context.Context, opts options, service sharing.Service, shareID string, draft *apiclient.ShareReleaseDraft,
) (*apiclient.ShareRelease, error) {
	release, err := service.Publish(ctx, shareID, draft)
	changed, ok := errors.AsType[*api.DraftChangedError](err)
	if !ok || !confirmedAlready(draft, &changed.Draft) {
		return release, err
	}
	fmt.Fprintf(opts.out, "\nThe project changed while it was being shared (%s). Its new build has the variables and "+
		"service key functions you confirmed, so it is shared instead.\n", theme.StripControl(changed.Err.Message))
	return service.Publish(ctx, shareID, &changed.Draft)
}

// confirmedAlready reports whether every variable and service key function a
// fresh draft lists was confirmed with the draft the owner reviewed.
func confirmedAlready(reviewed, fresh *apiclient.ShareReleaseDraft) bool {
	confirmedVariables := sharing.VariableNames(reviewed.Variables)
	for _, name := range sharing.VariableNames(fresh.Variables) {
		if !slices.Contains(confirmedVariables, name) {
			return false
		}
	}
	for _, name := range fresh.ServiceKeyFunctions {
		if !slices.Contains(reviewed.ServiceKeyFunctions, name) {
			return false
		}
	}
	return true
}

// confirmShare asks the owner to confirm the functions that get each copy's
// service key, then the share. --yes confirms both and says so.
func confirmShare(opts options, draft *apiclient.ShareReleaseDraft, ask bool) error {
	keyed := namesOrNone(draft.ServiceKeyFunctions)
	if !ask {
		if len(draft.ServiceKeyFunctions) > 0 {
			output.Warning(opts.out, "Each copy's service key, which bypasses row-level security, goes to: %s (confirmed by --yes).", keyed)
		}
		return nil
	}
	fmt.Fprintln(opts.out)
	if len(draft.ServiceKeyFunctions) > 0 {
		confirmed, err := confirm.Action(opts.in, opts.out,
			fmt.Sprintf("These functions read VOLCANO_SERVICE_KEY, so each copy gives them its own service key, "+
				"which bypasses row-level security: %s.", keyed),
			"Give them the service key?")
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(opts.out, "Nothing was shared. Sharing needs the service key functions confirmed.")
			return errDeclined
		}
	}
	confirmed, err := confirm.Action(opts.in, opts.out,
		"Anyone with the link can fork their own copy of what is listed above.", "Share it?")
	if err != nil {
		return err
	}
	if !confirmed {
		return errDeclined
	}
	return nil
}

func printShared(opts options, share *apiclient.Share, release *apiclient.ShareRelease) error {
	id := theme.StripControl(share.Id)
	if opts.jsonOutput {
		return writeJSON(opts.stdout, result{
			Share: share, Release: release,
			URL:      output.ShareURL(id),
			Markdown: output.ShareMarkdownSnippet(id),
			HTML:     output.ShareHTMLSnippet(id),
		})
	}
	out := opts.stdout
	fmt.Fprintln(out)
	if share.Serving {
		output.Success(out, "Shared. Anyone can fork it from: %s", output.ShareURL(id))
	} else {
		output.Success(out, "Published. Its link is %s", output.ShareURL(id))
		output.Warning(out, "Sharing is %s.", output.SharingState(share))
	}
	fmt.Fprintf(out, "Forking from the CLI: %s\n", cliruntime.CommandPath(cliruntime.Deps{}, "fork "+output.ShareURL(id)))
	output.ShareSnippets(out, id)
	return nil
}

// putDirectoryOnCloud makes the cloud project run what is in the working
// directory before it is shared: a directory not on Volcano Cloud yet gets a
// cloud project, linked as connect links it, and the directory is deployed as
// cloud deploy deploys it. Whatever a failed step leaves stays, and running the
// command again picks up from the linked project.
func putDirectoryOnCloud(ctx context.Context, opts options, dir directory, ask bool) error {
	if !dir.project() || opts.noDeploy {
		return nil
	}
	on := theme.On(opts.out)
	if dir.link == nil {
		name := filepath.Base(dir.path)
		if ask {
			confirmed, err := confirm.Action(opts.in, opts.out,
				"This project isn't on Volcano Cloud yet.",
				fmt.Sprintf("Create cloud project %s, deploy it, and share it?", name))
			if err != nil {
				return err
			}
			if !confirmed {
				return errDeclined
			}
		}
		fmt.Fprintf(opts.out, "\n%s\n", theme.Title("[1/3] Create the cloud project and link this directory", on))
		if err := opts.connect(ctx, opts); err != nil {
			return fmt.Errorf("creating the cloud project: %w\n\nNothing was deployed or shared. Fix the problem and run %s again",
				err, cliruntime.CommandPath(cliruntime.Deps{}, "share"))
		}
		fmt.Fprintf(opts.out, "\n%s\n", theme.Title("[2/3] Deploy this directory to it", on))
		if err := deployStep(ctx, opts); err != nil {
			return err
		}
		fmt.Fprintf(opts.out, "\n%s\n", theme.Title("[3/3] Share it", on))
		return nil
	}

	project := projectLabel(dir.link.Project)
	if opts.deploysFromGit(ctx, opts) {
		output.Warning(opts.out, "%s deploys from its Git repository, so this directory is not deployed: "+
			"the share is what the project runs now. Push to its production branch to share newer code.", project)
		return nil
	}
	if ask {
		confirmed, err := confirm.Action(opts.in, opts.out,
			fmt.Sprintf("Deploys this directory to %s, then shares it.", project), "Deploy and share?")
		if err != nil {
			return err
		}
		if !confirmed {
			return errDeclined
		}
	}
	fmt.Fprintf(opts.out, "\n%s\n", theme.Title("[1/2] Deploy this directory to "+project, on))
	if err := deployStep(ctx, opts); err != nil {
		return err
	}
	fmt.Fprintf(opts.out, "\n%s\n", theme.Title("[2/2] Share it", on))
	return nil
}

func projectLabel(project config.ProjectConfig) string {
	if name := theme.StripControl(project.Name); name != "" {
		return name
	}
	return theme.StripControl(project.ID)
}

func deployStep(ctx context.Context, opts options) error {
	if err := opts.deploy(ctx, opts); err != nil {
		return fmt.Errorf("deploying to the cloud project: %w\n\nThe cloud project exists and this directory is linked to it; "+
			"nothing was shared. Fix the problem and run %s again: it uses the linked project",
			err, cliruntime.CommandPath(cliruntime.Deps{}, "share"))
	}
	return nil
}

// runConnect creates a cloud project for the working directory and links it,
// by running volcano connect . without installing agent skills.
func runConnect(ctx context.Context, opts options) error {
	return execute(ctx, connectcmd.New(opts.deps), []string{".", "--skip-skills"}, opts) //nolint:contextcheck // ExecuteContext hands ctx to the command.
}

// runCloudDeploy deploys the working directory to its linked cloud project, by
// running volcano cloud deploy. It sends no env file unless --env-file names
// one: volcano/volcano.env holds the local server's values.
func runCloudDeploy(ctx context.Context, opts options) error {
	deps := opts.deps
	deps.CommandPathPrefix = "volcano cloud"
	args := []string{"--no-env-file"}
	if opts.envFile != "" {
		args = []string{"--env-file", opts.envFile}
	}
	return execute(ctx, deploycmd.New(deps), args, opts) //nolint:contextcheck // ExecuteContext hands ctx to the command.
}

func execute(ctx context.Context, cmd *cobra.Command, args []string, opts options) error {
	cmd.SetArgs(args)
	cmd.SetIn(opts.in)
	cmd.SetOut(opts.out)
	cmd.SetErr(opts.errOut)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	return cmd.ExecuteContext(ctx)
}

// workingDirectory reports what the working directory is: a directory linked
// to a cloud project, or one holding a volcano/ directory, is a project.
func workingDirectory() directory {
	if link, err := config.FindLinkFromWorkingDir(); err == nil && link != nil {
		return directory{path: link.Dir, link: link}
	}
	wd, err := os.Getwd()
	if err != nil {
		return directory{}
	}
	if info, err := os.Stat(filepath.Join(wd, "volcano")); err != nil || !info.IsDir() {
		return directory{}
	}
	return directory{path: wd}
}

// deploysFromGit reports a cloud project whose Git repository became its
// source, which refuses direct deploys. A failed read reports false, and the
// deploy then says what refused it.
func deploysFromGit(ctx context.Context, opts options) bool {
	// cloud deploy refuses a code deploy in every mode but platform, including
	// a project partway through moving to its Git repository.
	mode, err := cliproject.NewService(opts.deps).SourceMode(ctx)
	return err == nil && mode != apiclient.ProjectSourceExportStateModePlatform
}

// pickShare returns the share --link names, else the project's newest
// switched-on share, else its newest, else nil when it has none.
func pickShare(ctx context.Context, service sharing.Service, link string) (*apiclient.Share, error) {
	if link != "" {
		shareID, err := sharing.ParseLink(link)
		if err != nil {
			return nil, fmt.Errorf("--link: %w", err)
		}
		return service.Get(ctx, shareID)
	}
	shares, err := service.List(ctx)
	if err != nil {
		return nil, err
	}
	var newest *apiclient.Share
	for i := range shares {
		share := &shares[i]
		active := share.Status == apiclient.ShareStatusActive
		switch {
		case newest == nil:
			newest = share
		case active && newest.Status != apiclient.ShareStatusActive:
			newest = share
		case active == (newest.Status == apiclient.ShareStatusActive) && share.CreatedAt.After(newest.CreatedAt):
			newest = share
		}
	}
	return newest, nil
}

// requireShare is pickShare for the commands that read or change a share: a
// project with none has not been shared.
func requireShare(ctx context.Context, service sharing.Service, link string) (*apiclient.Share, error) {
	share, err := pickShare(ctx, service, link)
	if err != nil {
		return nil, err
	}
	if share == nil {
		return nil, fmt.Errorf("this project is not shared yet. Share it with %s",
			cliruntime.CommandPath(cliruntime.Deps{}, "share"))
	}
	return share, nil
}

// checkDescription trims a description and refuses one the API would.
func checkDescription(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("--description must be one line")
	}
	if n := len([]rune(value)); n > maxDescriptionLength {
		return "", fmt.Errorf("--description is %d characters; use at most %d", n, maxDescriptionLength)
	}
	return value, nil
}

func namesOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	cleaned := make([]string, len(names))
	for i, name := range names {
		cleaned[i] = theme.StripControl(name)
	}
	return strings.Join(cleaned, ", ")
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
