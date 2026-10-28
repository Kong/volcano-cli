// Package connectcmd wires the volcano connect command: one command from no
// login to a directory linked to a cloud project, with agent skills, an env
// file and AGENTS.md in place.
package connectcmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	cliauth "github.com/Kong/volcano-cli/internal/auth"
	initcmd "github.com/Kong/volcano-cli/internal/cmd/init"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	"github.com/Kong/volcano-cli/internal/projectinit"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	clisession "github.com/Kong/volcano-cli/internal/session"
	"github.com/Kong/volcano-cli/internal/setup"
	"github.com/Kong/volcano-cli/internal/symlink"
)

const (
	agentsFileName = "AGENTS.md"
	// projectReadyTimeout bounds the wait for a new project to become active.
	projectReadyTimeout = 2 * time.Minute
	projectPollInterval = 2 * time.Second

	dirMode  fs.FileMode = 0o755
	fileMode fs.FileMode = 0o644
)

type options struct {
	deps       cliruntime.Deps
	dir        string
	deviceCode string
	project    string
	name       string
	template   string
	signup     bool
	email      string
	skipSkills bool
	jsonOutput bool
	in         io.Reader
	out        io.Writer
	// log receives progress. Under --json it is stderr, so stdout holds only
	// the JSON result.
	log io.Writer
}

// result is what connect did, printed as JSON under --json.
type result struct {
	Project    projectRef `json:"project"`
	Created    bool       `json:"created"`
	Directory  string     `json:"directory"`
	LinkFile   string     `json:"link_file"`
	EnvFile    string     `json:"env_file"`
	AgentsFile string     `json:"agents_file"`
	Skills     []string   `json:"skills,omitempty"`
	// SkillLocations says where each harness has the skills, so an agent that
	// started before they were installed can read them.
	SkillLocations []skillLocation `json:"skill_locations,omitempty"`
	// SkillsNote is the line to follow when the skills are not loaded in the
	// running session.
	SkillsNote   string `json:"skills_note,omitempty"`
	DashboardURL string `json:"dashboard_url"`
}

// skillLocation is where one harness has the Volcano skills: a directory of
// <name>/SKILL.md folders for a file-drop harness, or a plugin, which needs an
// agent restart to load when connect installed or updated it.
type skillLocation struct {
	Harness         string `json:"harness"`
	Path            string `json:"path,omitempty"`
	Plugin          string `json:"plugin,omitempty"`
	RestartRequired bool   `json:"restart_required,omitempty"`
}

type projectRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// New returns the connect command.
func New(deps cliruntime.Deps) *cobra.Command {
	opts := options{deps: deps}
	cmd := &cobra.Command{
		Use:   "connect [directory]",
		Short: "Sign in and link a directory to a new cloud project",
		Long: `Sign in, create a cloud project, and set up a directory for it, ready for a
coding agent to build on.

connect:
  1. Signs in: takes --device-code from the dashboard's prompt, reuses a
     valid login, or runs browser login.
  2. Picks the project. With no directory argument, it creates a new project
     (--name, or a generated name like fuji-amber-42) and a new directory
     named after it. With a directory argument, the new project is named after
     the directory. If the name is taken it adds -2, -3, and so on. With
     --project <project-id> it uses that project instead. Without --project,
     run inside a linked directory, it keeps the linked project.
  3. Links the directory in .volcano/project.json. Commands run inside the
     directory use this project. Right after the link, it gitignores the env
     files and, for a new link, the link, unless .gitignore already has the
     entry or its negation.
  4. Creates the volcano/ scaffold if it is missing, and adds --template's
     starter. If some of the starter's files are missing and another differs
     from the file already there, connect stops before it changes anything.
     When every starter file is there, a rerun keeps the ones you changed.
  5. Writes the project's API URL and anon key to volcano/cloud.env.
  6. Adds a Volcano section to AGENTS.md.
  7. Installs the Volcano skills for every coding agent on this machine.

It ends with the project's name, its directory, and its dashboard link.

  volcano connect                          New project in a new directory
  volcano connect --name shop              New project and directory named shop
  volcano connect .                        Link this directory to a new project
  volcano connect my-app                   Create ./my-app and link it
  volcano connect --project <project-id>   Set up an existing project
  volcano connect --signup                 New to Volcano: create an account first`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.dir = args[0]
			}
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			opts.log = cmd.OutOrStdout()
			if opts.jsonOutput {
				opts.log = cmd.ErrOrStderr()
			}
			return run(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.deviceCode, "device-code", "",
		"Sign in with the code in the dashboard's prompt instead of the browser")
	cmd.Flags().StringVar(&opts.project, "project", "", "Use the existing project with this ID instead of creating one")
	cmd.Flags().StringVar(&opts.name, "name", "", "Name for the new project (default: the directory argument's name, else a generated name)")
	cmd.Flags().StringVar(&opts.template, "template", "", "Starter to scaffold: nextjs, javascript, python, ruby (default: base scaffold)")
	cmd.Flags().BoolVar(&opts.signup, "signup", false, "Create a Volcano account in the browser before connecting")
	cmd.Flags().StringVar(&opts.email, "email", "", "Email for --signup (default: git config user.email)")
	cmd.Flags().BoolVar(&opts.skipSkills, "skip-skills", false, "Don't install the Volcano skills")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print the result as JSON")
	cmd.MarkFlagsMutuallyExclusive("device-code", "signup")
	cmd.MarkFlagsMutuallyExclusive("project", "name")
	return cmd
}

func run(ctx context.Context, opts options) error {
	starter, err := initcmd.ResolveStarter(opts.template, "")
	if err != nil {
		return err
	}
	// Checked before sign-in: a typo found at the scaffold step would leave a
	// new project behind.
	if err := projectinit.CheckStarter(starter); err != nil {
		return fmt.Errorf("--template: %w (supported: nextjs, javascript, python, ruby)", err)
	}

	// Every command after sign-in takes VOLCANO_TOKEN over the saved login, so
	// a new login would be saved and then not used. Checked before the
	// dashboard's device code is spent.
	if strings.TrimSpace(os.Getenv("VOLCANO_TOKEN")) != "" &&
		(strings.TrimSpace(opts.deviceCode) != "" || opts.signup) {
		return errors.New("VOLCANO_TOKEN is set and would be used instead of the new login. " +
			"Unset it, or run connect without --device-code and --signup to use it")
	}
	// VOLCANO_PROJECT_ID is deprecated, and would override the link connect
	// writes. Checked before sign-in, so the dashboard's device code isn't spent.
	if strings.TrimSpace(os.Getenv("VOLCANO_PROJECT_ID")) != "" {
		return errors.New("VOLCANO_PROJECT_ID is set, and volcano connect does not use it. " +
			"Unset it, and pass --project <project-id> to set up an existing project")
	}

	// A --project that isn't an ID would fail only after the device code
	// was spent.
	if strings.TrimSpace(opts.project) != "" {
		if _, err := projectID(opts.project); err != nil {
			return err
		}
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	// A directory that can't be created would fail only after the project
	// was created.
	if err := checkTargetPath(opts); err != nil {
		return err
	}
	// Checked before sign-in too: a device code spent on a run that then
	// stops at the link can't be used again.
	if err := refuseBadLink(opts, wd); err != nil {
		return err
	}
	if err := refuseNameForLink(opts, wd); err != nil {
		return err
	}

	// A link connect can't use must not stop it signing in: resolveTarget
	// decides what to do about it.
	if opts.deps.ConfigLoader == nil {
		opts.deps.ConfigLoader = func() (*config.Config, error) {
			cfg, _, err := config.LoadIgnoringBadLink()
			return cfg, err
		}
	}
	sessions := clisession.NewFactory(opts.deps)
	// The directory is known now unless connect names it after a project.
	if dir := knownTarget(opts, wd); dir != "" {
		if err := checkStarter(dir, starter); err != nil {
			return err
		}
		// Found only at the write otherwise, after the device code is spent
		// and a project created.
		if err := refuseSymlinks(dir); err != nil {
			return err
		}
	}
	if err := ensureSession(ctx, opts, sessions); err != nil {
		return err
	}
	authenticated, err := sessions.Authenticated()
	if err != nil {
		return err
	}
	apiURL := sessions.APIURL(authenticated.Config)

	dir, project, created, err := resolveTarget(ctx, opts, authenticated, wd)
	if err != nil {
		return err
	}
	if created {
		output.Success(opts.log, "Created project %s (%s)", project.Name, project.Id)
	} else {
		output.Success(opts.log, "Using project %s (%s)", project.Name, project.Id)
	}
	// Checked again for the directory named after the project, before the
	// link or any file changes.
	if err := checkStarter(dir, starter); err != nil {
		return err
	}
	if err := refuseSymlinks(dir); err != nil {
		if created {
			return createdProjectError(opts, wd, dir, project, err)
		}
		return err
	}
	if err := waitUntilActive(ctx, authenticated.API, project); err != nil {
		if created && project.Status != apiclient.ProjectStatusFailed && project.Status != apiclient.ProjectStatusDeleting {
			return createdProjectError(opts, wd, dir, project, err)
		}
		return err
	}

	// Only `connect <directory> --project` relinks a directory linked to
	// another project that still exists.
	previous, _ := config.FindLink(dir)
	// Before a new link is written, so a failure here never leaves one
	// without its entry. A team that commits the link removes its .gitignore
	// entry, so a rerun on an existing link doesn't add it back.
	newLink := previous == nil || previous.Dir != dir
	if err := ensureGitignored(dir, newLink); err != nil {
		if created {
			return createdProjectError(opts, wd, dir, project, err)
		}
		return err
	}
	link, err := cliproject.Link(dir, project)
	if err != nil {
		if created {
			return createdProjectError(opts, wd, dir, project, err)
		}
		return err
	}
	output.Success(opts.log, "Linked %s", relPath(wd, link.Path()))
	// A rerun from wd would not find a link in a new directory below it and
	// would create another project, so later failures name the directory.
	setupError := func(err error) error {
		if created && displayDir(wd, dir) != "." {
			return linkedProjectError(opts, wd, dir, project, err)
		}
		return err
	}
	if previous != nil && previous.Dir == dir && previous.Project.ID != project.Id.String() && !created {
		shown := displayDir(wd, dir)
		output.Warning(opts.log, "%s was linked to %s. It is now linked to %s. To undo, run: %s",
			shown, projectLabel(previous.Project), projectLabel(config.ProjectConfig{ID: project.Id.String(), Name: project.Name}),
			cliruntime.CommandPath(opts.deps, "connect "+cliruntime.ShellQuote(shown)+" --project "+previous.Project.ID))
	}

	if err := scaffold(opts, dir, starter); err != nil {
		return setupError(err)
	}

	anonKey, err := cliproject.DefaultAnonKey(ctx, authenticated.API, project.Id)
	if err != nil {
		return setupError(err)
	}
	envPath := cliproject.CloudEnvPath(dir)
	if err := cliproject.WriteCloudEnv(dir, apiURL, anonKey); err != nil {
		return setupError(err)
	}
	output.Success(opts.log, "Wrote the project's API URL and anon key to %s", cliproject.CloudEnvRelPath)

	// AGENTS.md is usually committed, so the web URL, which VOLCANO_WEB_URL
	// can set, is used without userinfo, query or fragment, as in cloud.env.
	webURL, err := api.PublicURL(authenticated.Config.WebURLForAPIURL(apiURL))
	if err != nil {
		return setupError(fmt.Errorf("the dashboard URL is not valid: %w", err))
	}
	dashboardURL := projectDashboardURL(webURL, project.Id)
	agentsPath := filepath.Join(dir, agentsFileName)
	// The shared AGENTS.md gets the dashboard's root, not this developer's project.
	dashboardRoot := strings.TrimRight(webURL, "/") + "/dashboard"
	if err := writeAgentsSection(dir, agentsSection(dashboardRoot)); err != nil {
		return setupError(err)
	}
	output.Success(opts.log, "Updated the Volcano section in %s", agentsFileName)

	skills, locations := installSkills(ctx, opts)

	res := result{
		Project:      projectRef{ID: project.Id.String(), Name: project.Name},
		Created:      created,
		Directory:    dir,
		LinkFile:     link.Path(),
		EnvFile:      envPath,
		AgentsFile:   agentsPath,
		Skills:       skills,
		DashboardURL: dashboardURL,

		SkillLocations: locations,
		SkillsNote:     skillsNote(locations),
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, res)
	}
	shown := displayDir(wd, dir)
	fmt.Fprintln(opts.out)
	fmt.Fprintf(opts.out, "Project:   %s\n", project.Name)
	fmt.Fprintf(opts.out, "Directory: %s\n", shown)
	fmt.Fprintf(opts.out, "Dashboard: %s\n", dashboardURL)
	if len(locations) > 0 {
		fmt.Fprintln(opts.out)
		fmt.Fprintln(opts.out, "Skills:")
		for _, l := range locations {
			fmt.Fprintf(opts.out, "  %s: %s\n", l.Harness, l.describe())
		}
		if res.SkillsNote != "" {
			fmt.Fprintln(opts.out, res.SkillsNote)
		}
	}
	fmt.Fprintln(opts.out)
	if shown == "." {
		fmt.Fprintln(opts.out, "Next: read AGENTS.md and use the Volcano skills to build.")
	} else {
		fmt.Fprintf(opts.out, "Next: cd %s, read AGENTS.md there, and use the Volcano skills to build.\n", cliruntime.ShellQuote(shown))
	}
	return nil
}

// ensureSession leaves a valid login saved: the --device-code given, the
// saved login if the API still accepts it, or a new browser login.
func ensureSession(ctx context.Context, opts options, sessions clisession.Factory) error {
	cfg, err := sessions.Config()
	if err != nil {
		return err
	}
	auth := cliauth.NewService(opts.deps)

	// A refused device code falls back to browser login, not to the saved
	// login: that may be another account, and the user who copied the prompt
	// expects the project in theirs.
	refused := false
	if deviceCode := strings.TrimSpace(opts.deviceCode); deviceCode != "" {
		// Before the code is spent on a login that cannot be saved.
		if err := checkConfigWritable(); err != nil {
			return err
		}
		// The project is looked up after sign-in, so a wrong --project fails
		// there with the login saved. LoginWithDeviceCode saves the login of
		// a spent code even when its own checks fail.
		_, err := auth.LoginWithDeviceCode(ctx, cfg, deviceCode, "", opts.log)
		if refusal, ok := errors.AsType[*cliauth.DeviceCodeRefusedError](err); ok {
			output.Warning(opts.log, "The code from the dashboard %s. Signing in in the browser instead.", refusal.Reason)
			refused = true
		} else {
			if _, saved := errors.AsType[*cliauth.LoginSavedError](err); saved {
				output.Note(opts.log, "The code from the dashboard is spent, but its login is saved. Run the command again without --device-code.")
			}
			if err != nil {
				return fmt.Errorf("device code authentication failed: %w", err)
			}
			// LoginWithDeviceCode has saved the login.
			output.Success(opts.log, "Signed in with the code from the dashboard")
			return nil
		}
	}

	if token := cfg.Token(); token != "" && !opts.signup && !refused {
		// A project access token can't be checked without a project; the
		// project lookup below reports it if it is wrong.
		if config.IsProjectToken(token) {
			return nil
		}
		client, err := sessions.APIClient(sessions.APIURL(cfg), token)
		if err != nil {
			return err
		}
		err = client.ValidateToken(ctx)
		if err == nil {
			output.Success(opts.log, "Signed in")
			return nil
		}
		if api.Status(err) != http.StatusUnauthorized || strings.TrimSpace(os.Getenv("VOLCANO_TOKEN")) != "" {
			return fmt.Errorf("failed to check the saved login: %w", err)
		}
		output.Note(opts.log, "The saved login is no longer valid. Signing in again.")
	}

	// A saved login is used without writing, so only a new login needs the
	// home to be writable. Checked before the browser or signup code is issued.
	if err := checkConfigWritable(); err != nil {
		return err
	}
	var credentials cliauth.Credentials
	if opts.signup {
		email := strings.TrimSpace(opts.email)
		if email == "" {
			email = cliauth.GitEmail(ctx, opts.deps)
		}
		if email == "" {
			return errors.New("--signup needs an email address: pass --email, or set git config --global user.email to an address that is not a GitHub noreply one")
		}
		credentials, err = auth.Signup(ctx, cfg, email, opts.log)
	} else {
		fmt.Fprintln(opts.log, "New to Volcano? Stop this and run `volcano connect --signup` to create an account.")
		credentials, err = auth.LoginWithBrowser(ctx, cfg, opts.log)
	}
	if err != nil {
		return fmt.Errorf("browser authentication failed: %w", err)
	}
	if err := cliauth.SaveCredentials(credentials); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}
	output.Success(opts.log, "Signed in")
	return nil
}

// checkConfigWritable stops connect before it issues a device or signup code
// when it could not save the login. The usual cause is an agent sandbox with
// no write access to the home directory.
func checkConfigWritable() error {
	err := config.CheckWritable()
	if err == nil {
		return nil
	}
	return fmt.Errorf("the Volcano login cannot be saved: %w. "+
		"This usually means a sandbox that cannot write to your home directory. "+
		"Run the command again outside the sandbox, or approve the escalation if your agent asks", err)
}

// maxNameSuffix bounds the -2, -3, ... suffixes tried when the project name
// is taken.
const maxNameSuffix = 20

// resolveTarget returns the directory to set up, its project, and whether
// connect created the project.
//
// A directory argument is used as given: --project links that project, a
// rerun keeps the project the directory is linked to, and otherwise a new
// project is named after the directory. Without a directory argument, a
// linked directory around the working directory keeps its project, and is the
// one --project sets up when the link names it. Otherwise connect picks the
// project first, --project or a new one, and sets it up in a new directory
// named after it.
//
// A link that can't be used is replaced only when the user says which project
// goes there: --project for an existing one, --name for a new one. Otherwise
// connect would create a project the user may not want, beside the one the
// link named.
func resolveTarget(ctx context.Context, opts options, session *clisession.Session, wd string) (string, *apiclient.Project, bool, error) {
	if opts.dir != "" {
		dir, err := filepath.Abs(opts.dir)
		if err != nil {
			return "", nil, false, err
		}
		var project *apiclient.Project
		created := false
		if strings.TrimSpace(opts.project) != "" {
			if _, err := findLink(opts, wd, dir); err != nil {
				return "", nil, false, err
			}
			project, err = existingProject(ctx, session.API, opts.project)
		} else {
			project, created, err = linkedOrNewProject(ctx, opts, session, wd, dir)
		}
		if err != nil {
			return "", nil, false, err
		}
		// Created only now, so a failed project lookup or create leaves no
		// empty directory behind.
		if err := makeDir(dir); err != nil {
			if created {
				return "", nil, false, createdProjectError(opts, wd, dir, project, err)
			}
			return "", nil, false, err
		}
		return dir, project, created, nil
	}

	if strings.TrimSpace(opts.project) != "" {
		project, err := existingProject(ctx, session.API, opts.project)
		if err != nil {
			return "", nil, false, err
		}
		// A rerun inside the project's linked directory sets that directory up
		// again, as a rerun without --project does.
		if link, _ := config.FindLink(wd); link != nil && link.Project.ID == project.Id.String() {
			return link.Dir, project, false, nil
		}
		dir := filepath.Join(wd, dirName(project.Name))
		if err := refuseSymlinkedDir(dir); err != nil {
			return "", nil, false, err
		}
		// Two project names can make the same directory name. Never relink a
		// directory that belongs to another project.
		link, err := findLink(opts, wd, dir)
		if err != nil {
			return "", nil, false, err
		}
		if link != nil && link.Dir == dir && link.Project.ID != project.Id.String() {
			return "", nil, false, fmt.Errorf("%s is linked to another project (%s). Pass a directory for %s: volcano connect <directory> --project %s",
				displayDir(wd, dir), link.Project.ID, project.Name, project.Id)
		}
		// Only a directory named here on purpose gets files in an existing,
		// unlinked directory, such as an unrelated repository.
		if (link == nil || link.Dir != dir) && !emptyOrMissing(dir) {
			shown := displayDir(wd, dir)
			return "", nil, false, fmt.Errorf("%s already exists and is not linked to %s. To set the project up in it, run: %s",
				shown, project.Name, cliruntime.CommandPath(opts.deps, "connect "+cliruntime.ShellQuote(shown)+" --project "+project.Id.String()))
		}
		return dir, project, false, makeDir(dir)
	}

	link, err := config.FindLink(wd)
	if bad, ok := errors.AsType[*config.LinkError](err); ok {
		// The tree is linked, but to nothing connect can read: set up the
		// directory that holds the link, as a valid link would.
		project, created, err := linkedOrNewProject(ctx, opts, session, wd, bad.Dir)
		return bad.Dir, project, created, err
	}
	if err != nil {
		return "", nil, false, err
	}
	if link != nil {
		project, created, err := linkedOrNewProject(ctx, opts, session, wd, link.Dir)
		return link.Dir, project, created, err
	}

	// Lstat, so a symlink is taken even when its target is missing.
	taken := func(name string) bool {
		_, err := os.Lstat(filepath.Join(wd, dirName(name)))
		return err == nil
	}
	project, err := newProject(ctx, opts, session, generateName(), taken)
	if err != nil {
		return "", nil, false, err
	}
	dir := filepath.Join(wd, dirName(project.Name))
	if err := refuseSymlinkedDir(dir); err != nil {
		return "", nil, false, createdProjectError(opts, wd, dir, project, err)
	}
	if err := makeDir(dir); err != nil {
		return "", nil, false, createdProjectError(opts, wd, dir, project, err)
	}
	return dir, project, true, nil
}

// refuseSymlinkedDir fails when dir, a directory connect named after a
// project, is a symlink. A cloned repository can commit one that points out
// of the checkout, and connect would write the link, cloud.env, .gitignore,
// and AGENTS.md there. A directory argument may be a symlink: the user chose
// it.
func refuseSymlinkedDir(dir string) error {
	// A missing directory is created, and any other error is reported when
	// connect creates it.
	if info, err := os.Lstat(dir); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return nil //nolint:nilerr // makeDir reports the error
	}
	return fmt.Errorf("%s is a symlink, and connect doesn't follow one to the directory it names after the project. "+
		"Remove the symlink, or pass the directory to set up: volcano connect <directory> --project <project-id>", dir)
}

func makeDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	return nil
}

// checkTargetPath fails when the directory argument, or the nearest of its
// parents that exists, is not a directory, so connect can't create it.
func checkTargetPath(opts options) error {
	if opts.dir == "" {
		return nil
	}
	dir, err := filepath.Abs(opts.dir)
	if err != nil {
		return err
	}
	if config.IsHomeDir(dir) {
		return config.ErrHomeLink
	}
	for path := dir; ; {
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() {
				return nil
			}
			if path == dir {
				return fmt.Errorf("%s is not a directory", dir)
			}
			return fmt.Errorf("can't create %s: %s is not a directory", dir, path)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil
		}
		path = parent
	}
}

// createdProjectError is err, a failure after connect created project and
// before it linked dir, with the command that sets the project up, so a rerun
// doesn't create another.
func createdProjectError(opts options, wd, dir string, project *apiclient.Project, err error) error {
	return fmt.Errorf("%w\nCreated project %s (%s), but did not link it. To set it up, run: %s", err,
		project.Name, project.Id, cliruntime.CommandPath(opts.deps,
			"connect "+cliruntime.ShellQuote(displayDir(wd, dir))+" --project "+project.Id.String()+starterFlag(opts)))
}

// linkedProjectError is err, a failure after connect created project and
// linked it in dir, a new directory below wd, with the command that finishes
// setting it up.
func linkedProjectError(opts options, wd, dir string, project *apiclient.Project, err error) error {
	return fmt.Errorf("%w\nCreated project %s (%s) and linked it in %s. To finish setting it up, run: %s", err,
		project.Name, project.Id, displayDir(wd, dir), cliruntime.CommandPath(opts.deps, "connect "+cliruntime.ShellQuote(displayDir(wd, dir))+starterFlag(opts)))
}

// starterFlag is the --template the user passed, for the commands connect
// prints, or "".
func starterFlag(opts options) string {
	if t := strings.TrimSpace(opts.template); t != "" {
		return " --template " + cliruntime.ShellQuote(t)
	}
	return ""
}

// projectID parses --project, which takes an ID only.
func projectID(raw string) (uuid.UUID, error) {
	raw = strings.TrimSpace(raw)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, fmt.Errorf("--project takes a project ID, got %q. Run `volcano projects list` to find it", raw)
	}
	return id, nil
}

// existingProject is the project --project names.
func existingProject(ctx context.Context, client *api.Client, raw string) (*apiclient.Project, error) {
	raw = strings.TrimSpace(raw)
	id, err := projectID(raw)
	if err != nil {
		return nil, err
	}
	project, err := client.GetProject(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get project %s: %w", raw, err)
	}
	return project, nil
}

// linkedOrNewProject returns the project dir is linked to, so a rerun doesn't
// create another, else a new project named after dir.
func linkedOrNewProject(ctx context.Context, opts options, session *clisession.Session, wd, dir string) (*apiclient.Project, bool, error) {
	link, err := findLink(opts, wd, dir)
	if err != nil {
		return nil, false, err
	}
	if link != nil && link.Dir == dir {
		id, parseErr := uuid.Parse(link.Project.ID)
		if parseErr != nil {
			return nil, false, fmt.Errorf("project link %s has an invalid project ID %q", link.Path(), link.Project.ID)
		}
		project, err := session.API.GetProject(ctx, id)
		if err == nil {
			return project, false, nil
		}
		if api.Status(err) != http.StatusNotFound {
			return nil, false, fmt.Errorf("failed to get the linked project: %w", err)
		}
		output.Warning(opts.log, "The linked project %s no longer exists. Creating a new one.", link.Project.ID)
	}
	// A limit hint without the directory would set up a new one inside it.
	if opts.dir == "" {
		opts.dir = displayDir(wd, dir)
	}
	project, err := newProject(ctx, opts, session, defaultProjectName(dir), nil)
	if err != nil {
		return nil, false, err
	}
	return project, true, nil
}

// findLink is config.FindLink for dir, the directory connect sets up, with a
// link it can't use in dir or a parent treated as no link. One in dir itself
// is replaced only with --project or --name; without either, findLink fails
// and names the file.
func findLink(opts options, wd, dir string) (*config.Link, error) {
	link, err := config.FindLink(dir)
	bad, ok := errors.AsType[*config.LinkError](err)
	if !ok {
		return link, err
	}
	switch {
	case bad.Dir != dir:
		output.Warning(opts.log, "Ignoring a project link that can't be used: %v", bad)
	case strings.TrimSpace(opts.project) != "" || strings.TrimSpace(opts.name) != "":
		output.Warning(opts.log, "Replacing a project link that can't be used: %v", bad)
	default:
		return nil, badLinkError(wd, bad)
	}
	return nil, nil //nolint:nilnil // the link is ignored, so there is none
}

// refuseBadLink is findLink's refusal, checked before sign-in: the directory
// connect would set up holds a link it can't use, and neither --project nor
// --name says what replaces it.
func refuseBadLink(opts options, wd string) error {
	if strings.TrimSpace(opts.project) != "" || strings.TrimSpace(opts.name) != "" {
		return nil
	}
	start := wd
	if opts.dir != "" {
		dir, err := filepath.Abs(opts.dir)
		if err != nil {
			return err
		}
		start = dir
	}
	_, err := config.FindLink(start)
	bad, ok := errors.AsType[*config.LinkError](err)
	// Without a directory argument, connect sets up the directory holding the
	// link, wherever it is. With one, a bad link above it is ignored.
	if !ok || (opts.dir != "" && bad.Dir != start) {
		return nil
	}
	return badLinkError(wd, bad)
}

// refuseNameForLink fails when --name asks for a new project but the
// directory connect sets up is linked, so connect would keep the linked
// project and drop --name. Checked before sign-in.
func refuseNameForLink(opts options, wd string) error {
	if strings.TrimSpace(opts.name) == "" {
		return nil
	}
	start := wd
	if opts.dir != "" {
		dir, err := filepath.Abs(opts.dir)
		if err != nil {
			return err
		}
		start = dir
	}
	// A link that can't be used is one --name replaces, so only a valid one
	// stops connect.
	link, _ := config.FindLink(start)
	if link == nil {
		return nil
	}
	// With a directory argument, a link above it doesn't stop a new project
	// in it.
	if opts.dir != "" && link.Dir != start {
		return nil
	}
	return fmt.Errorf("%s is linked to %s, and connect keeps a linked directory's project, so --name would be ignored. "+
		"Run connect without --name to set it up, or outside the linked directory to create a new project",
		displayDir(wd, link.Dir), projectLabel(link.Project))
}

func badLinkError(wd string, bad *config.LinkError) error {
	shown := displayDir(wd, bad.Dir)
	return fmt.Errorf("%w. To link %s to its project again, run: volcano connect %s --project <project-id>. "+
		"To link it to a new project, add --name <name>, or delete %s", bad, shown, cliruntime.ShellQuote(shown), bad.Path())
}

// newProject creates a project named --name, else what the user types at a
// prompt, else fallback. A name taken() reports, or one the API says exists,
// gets a suffix.
func newProject(ctx context.Context, opts options, session *clisession.Session, fallback string, taken func(string) bool) (*apiclient.Project, error) {
	if err := session.Config.RequireAccountToken(); err != nil {
		return nil, fmt.Errorf("%w. To link the token's project, pass --project <project-id>", err)
	}
	name, err := projectName(opts, fallback)
	if err != nil {
		return nil, err
	}
	return createProject(ctx, opts, session.API, name, taken)
}

// createProject creates a project named name. When the name is taken, by an
// existing project or by taken(), it tries name-2, name-3, and so on, so
// connect always gets a new project.
func createProject(ctx context.Context, opts options, client *api.Client, name string, taken func(string) bool) (*apiclient.Project, error) {
	candidate := name
	for suffix := 2; ; suffix++ {
		if taken != nil && taken(candidate) {
			if suffix > maxNameSuffix {
				return nil, fmt.Errorf("%s through %s are taken. Pass --name with another name", name, candidate)
			}
			candidate = fmt.Sprintf("%s-%d", name, suffix)
			continue
		}
		project, err := client.CreateProject(ctx, candidate, "")
		switch {
		case err == nil:
			if candidate != name {
				output.Note(opts.log, "%s is taken, so the new project is %s.", name, candidate)
			}
			return project, nil
		case api.Status(err) == http.StatusConflict && suffix <= maxNameSuffix:
			candidate = fmt.Sprintf("%s-%d", name, suffix)
		case isProjectLimit(err):
			return nil, projectLimitError(ctx, client, opts, err)
		default:
			return nil, fmt.Errorf("failed to create project %q: %w", candidate, err)
		}
	}
}

// projectName is --name, else what the user types at a prompt, else fallback.
// It asks only when stdin is a terminal and none of the environment markers
// `volcano setup` also checks (CI, VOLCANO_NONINTERACTIVE, TERM=dumb) is set.
// --device-code comes from the dashboard's prompt for a coding agent, which
// may run commands in a terminal but can't answer, so it never asks.
func projectName(opts options, fallback string) (string, error) {
	if name := strings.TrimSpace(opts.name); name != "" {
		return name, nil
	}
	if opts.jsonOutput || strings.TrimSpace(opts.deviceCode) != "" || confirm.MarkedNonInteractive() || !confirm.CanPrompt(opts.in) {
		return fallback, nil
	}
	fmt.Fprintf(opts.log, "Project name [%s]: ", fallback)
	line, err := bufio.NewReader(opts.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if name := strings.TrimSpace(line); name != "" {
		return name, nil
	}
	return fallback, nil
}

var unsafeNameChars = regexp.MustCompile(`[^a-z0-9-]+`)

// defaultProjectName is dir's name made safe for a project name.
func defaultProjectName(dir string) string {
	return dirName(filepath.Base(dir))
}

// dirName makes name safe as a project name and a directory name: lowercase
// letters, digits and dashes.
func dirName(name string) string {
	name = unsafeNameChars.ReplaceAllString(strings.ToLower(name), "-")
	name = strings.Trim(name, "-")
	if len(name) < 2 {
		return "my-app"
	}
	return name
}

func isProjectLimit(err error) bool {
	return api.Status(err) == http.StatusForbidden && strings.Contains(strings.ToLower(api.Message(err)), "project limit")
}

// projectLimitError explains a create refused by the plan's project limit and
// lists the projects the user could link instead. The free plan allows one
// project.
func projectLimitError(ctx context.Context, client *api.Client, opts options, limitErr error) error {
	page, err := client.ListProjects(ctx, api.DefaultPage, api.DefaultLimit)
	if err != nil || len(page.Data) == 0 {
		return fmt.Errorf("%w\nDelete a project or upgrade your plan to create another", limitErr)
	}
	// The hint keeps the directory and starter the user asked for: without
	// the directory, --project sets up a new one named after the project.
	command := "connect"
	if opts.dir != "" {
		command += " " + cliruntime.ShellQuote(opts.dir)
	}
	var list strings.Builder
	for _, p := range page.Data {
		fmt.Fprintf(&list, "\n  - %s: %s --project %s%s", p.Name, cliruntime.CommandPath(opts.deps, command), p.Id, starterFlag(opts))
	}
	return fmt.Errorf("%w\nDelete a project or upgrade your plan to create another, or link an existing project:%s", limitErr, list.String())
}

func waitUntilActive(ctx context.Context, client *api.Client, project *apiclient.Project) error {
	deadline := time.Now().Add(projectReadyTimeout)
	for project.Status != apiclient.ProjectStatusActive {
		if project.Status == apiclient.ProjectStatusFailed || project.Status == apiclient.ProjectStatusDeleting {
			return fmt.Errorf("project %s is %s", project.Name, project.Status)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("project %s is still %s after %s", project.Name, project.Status, projectReadyTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(projectPollInterval):
		}
		next, err := client.GetProject(ctx, project.Id)
		if err != nil {
			return fmt.Errorf("failed to check project %s: %w", project.Name, err)
		}
		*project = *next
	}
	return nil
}

// scaffold creates the volcano/ scaffold when it is missing, and adds the
// starter --template names. A rerun without --template leaves an existing
// scaffold alone. Base scaffold files that exist are kept either way, and the
// starter's .gitignore lines are merged into one connect wrote; see
// projectinit.RunStarterIn.
//
// A failed --template fails connect, which checkStarter makes happen before
// any change. The base scaffold is a convenience, so a failure is a warning.
func scaffold(opts options, dir, starter string) error {
	if starter == "" && exists(filepath.Join(dir, "volcano")) {
		return nil
	}
	result, err := projectinit.RunStarterIn(dir, starter)
	if err != nil && starter != "" {
		return starterError(dir, starter, err)
	}
	if err != nil {
		if conflicts, ok := projectinit.ConflictMessages(err); ok {
			output.Warning(opts.log, "Skipped the scaffold: %s. Run `volcano init` to see the conflicts.", strings.Join(conflicts, "; "))
			return nil
		}
		output.Warning(opts.log, "Skipped the scaffold: %v", err)
		return nil
	}
	if created := result.Created(); len(created) > 0 {
		output.Success(opts.log, "Created the scaffold (%d files)", len(created))
	}
	if kept := result.Kept(); len(kept) > 0 {
		output.Note(opts.log, "Kept the changed %s starter files: %s.", starter, strings.Join(kept, ", "))
	}
	return nil
}

// checkStarter fails when --template's starter can't be added to dir, before
// connect changes anything there. A directory that doesn't exist yet can't
// conflict.
func checkStarter(dir, starter string) error {
	if starter == "" || !exists(dir) {
		return nil
	}
	if err := projectinit.CheckStarterIn(dir, starter); err != nil {
		return starterError(dir, starter, err)
	}
	return nil
}

func starterError(dir, starter string, err error) error {
	conflicts, ok := projectinit.ConflictMessages(err)
	if !ok {
		return fmt.Errorf("can't add the %s starter to %s: %w", starter, dir, err)
	}
	return fmt.Errorf("can't add the %s starter to %s, because these files differ from it: %s. "+
		"Run connect without --template to keep them, or move them and try again",
		starter, dir, strings.Join(conflicts, "; "))
}

// refuseSymlinks fails when a file connect writes in dir, or a directory
// holding one, is a symlink.
func refuseSymlinks(dir string) error {
	for _, rel := range []string{config.LinkRelPath, filepath.FromSlash(cliproject.CloudEnvRelPath), ".gitignore", agentsFileName} {
		if err := symlink.Refuse(dir, rel); err != nil {
			return err
		}
	}
	return nil
}

// knownTarget is the directory connect will set up when it is known before
// sign-in: the directory argument, or the linked directory around the working
// directory that connect keeps. It is "" when connect names the directory
// after a project.
func knownTarget(opts options, wd string) string {
	if opts.dir != "" {
		dir, err := filepath.Abs(opts.dir)
		if err != nil {
			return ""
		}
		return dir
	}
	link, err := config.FindLink(wd)
	if bad, ok := errors.AsType[*config.LinkError](err); ok && strings.TrimSpace(opts.project) == "" {
		// resolveTarget sets up the directory that holds a link it can't use.
		return bad.Dir
	}
	if link == nil {
		return ""
	}
	if raw := strings.TrimSpace(opts.project); raw != "" {
		if id, err := uuid.Parse(raw); err != nil || id.String() != link.Project.ID {
			return ""
		}
	}
	return link.Dir
}

// installSkills installs the Volcano skills for every coding agent detected on
// this machine, as `volcano setup --yes` does, and returns the agents it
// installed for. A failed install is a warning: connect has already done its
// job.
func installSkills(ctx context.Context, opts options) ([]string, []skillLocation) {
	if opts.skipSkills {
		return nil, nil
	}
	report, err := setup.Run(ctx, setup.Options{HTTPDoer: opts.deps.HTTPClient})
	if err != nil {
		output.Warning(opts.log, "Could not install the Volcano skills: %v. Run `volcano setup` to retry.", err)
		return nil, nil
	}
	setup.RenderReport(opts.log, report)
	return installedFrom(report)
}

// installedFrom is the harnesses a setup report installed for, and where each
// has the skills.
func installedFrom(report setup.Report) ([]string, []skillLocation) {
	var installed []string
	var locations []skillLocation
	for _, r := range report.Results {
		switch r.Status {
		case setup.StatusInstalled, setup.StatusUpdated, setup.StatusUpToDate:
			installed = append(installed, r.Harness)
			if r.Dir != "" || r.Plugin != "" {
				// A plugin that was already current is loaded in an agent that
				// started after it was installed; a new or updated one needs a
				// restart.
				restart := r.Plugin != "" && r.Status != setup.StatusUpToDate
				locations = append(locations, skillLocation{Harness: r.Harness, Path: r.Dir, Plugin: r.Plugin, RestartRequired: restart})
			}
		}
	}
	return installed, locations
}

// describe is the location as one line of the closing output.
func (l skillLocation) describe() string {
	if l.Path != "" {
		return l.Path
	}
	if l.RestartRequired {
		return "plugin " + l.Plugin + ", loaded after your agent restarts"
	}
	return "plugin " + l.Plugin + ", already up to date"
}

// skillsNote is the one line telling an agent what to do when the skills it
// just had installed are not loaded in its running session, which started
// before they existed.
func skillsNote(locations []skillLocation) string {
	hasPath, hasPlugin := false, false
	for _, l := range locations {
		hasPath = hasPath || l.Path != ""
		hasPlugin = hasPlugin || l.RestartRequired
	}
	switch {
	case hasPath && hasPlugin:
		return "If the Volcano skills are not loaded in your running session, read the SKILL.md files under the paths above, or restart your agent for the plugin."
	case hasPath:
		return "If the Volcano skills are not loaded in your running session, read the SKILL.md files under the paths above."
	case hasPlugin:
		return "If the Volcano skills are not loaded in your running session, restart your agent, or run `volcano setup --manual` and read the SKILL.md files under ~/.volcano/skills."
	}
	return ""
}

// displayDir is dir as the user would type it from wd: "." for wd itself,
// ./name below it, and the absolute path elsewhere.
func displayDir(wd, dir string) string {
	rel, err := filepath.Rel(wd, dir)
	switch {
	case err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)):
		return dir
	case rel == ".":
		return "."
	default:
		return "." + string(filepath.Separator) + rel
	}
}

// projectLabel is "name (id)", or the ID alone when the name is unknown.
func projectLabel(project config.ProjectConfig) string {
	if project.Name == "" {
		return project.ID
	}
	return project.Name + " (" + project.ID + ")"
}

func relPath(base, path string) string {
	if rel, err := filepath.Rel(base, path); err == nil {
		return rel
	}
	return path
}

// projectDashboardURL is the project's overview page in the dashboard.
func projectDashboardURL(webURL string, projectID uuid.UUID) string {
	return strings.TrimRight(webURL, "/") + "/dashboard/overview?" + url.Values{"project": []string{projectID.String()}}.Encode()
}

// emptyOrMissing reports whether dir is missing or an empty directory.
func emptyOrMissing(dir string) bool {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	return err == nil && len(entries) == 0
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
