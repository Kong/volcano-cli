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
	token      string
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
	Project      projectRef `json:"project"`
	Created      bool       `json:"created"`
	Directory    string     `json:"directory"`
	LinkFile     string     `json:"link_file"`
	EnvFile      string     `json:"env_file"`
	AgentsFile   string     `json:"agents_file"`
	Skills       []string   `json:"skills,omitempty"`
	DashboardURL string     `json:"dashboard_url"`
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
  1. Signs in: reuses a valid login, takes --token, or runs browser login.
  2. Picks the project. With no directory argument, it creates a new project
     (--name, or a generated name like fuji-amber-42) and a new directory
     named after it. With a directory argument, the new project is named after
     the directory. If the name is taken it adds -2, -3, and so on. With
     --project <project-id> it uses that project instead. Run inside a linked
     directory, it keeps the linked project.
  3. Links the directory in .volcano/project.json. Commands run inside the
     directory use this project.
  4. Creates the volcano/ scaffold if it is missing, and adds --template's
     starter. A starter file that differs from one already there stops
     connect before it changes anything.
  5. Writes the project's API URL and anon key to volcano/cloud.env.
  6. Gitignores the link and the env files, and adds a Volcano section to
     AGENTS.md.
  7. Installs the Volcano skills for every coding agent on this machine.

It ends with the project's name, its directory, and its dashboard link.

  volcano connect                          New project in a new directory
  volcano connect --name shop              New project and directory named shop
  volcano connect .                        Link this directory to a new project
  volcano connect my-app                   Create ./my-app and link it
  volcano connect --project <project-id>   Set up an existing project
  volcano connect --signup                 New to Volcano: create an account first
  npx -y @volcano.dev/cli connect          No install needed`,
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
	cmd.Flags().StringVar(&opts.token, "token", "", "Sign in with a token instead of the browser")
	cmd.Flags().StringVar(&opts.project, "project", "", "Use the existing project with this ID instead of creating one")
	cmd.Flags().StringVar(&opts.name, "name", "", "Name for the new project (default: the directory argument's name, else a generated name)")
	cmd.Flags().StringVar(&opts.template, "template", "", "Starter to scaffold: nextjs, javascript, python, ruby (default: base scaffold)")
	cmd.Flags().BoolVar(&opts.signup, "signup", false, "Create a Volcano account in the browser before connecting")
	cmd.Flags().StringVar(&opts.email, "email", "", "Email for --signup (default: git config user.email)")
	cmd.Flags().BoolVar(&opts.skipSkills, "skip-skills", false, "Don't install the Volcano skills")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print the result as JSON")
	cmd.MarkFlagsMutuallyExclusive("token", "signup")
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
	// one-time token is spent.
	if strings.TrimSpace(os.Getenv("VOLCANO_TOKEN")) != "" && (strings.TrimSpace(opts.token) != "" || opts.signup) {
		return errors.New("VOLCANO_TOKEN is set and would be used instead of the new login. Unset it, or run connect without --token and --signup to use it")
	}

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	// Checked before sign-in too: a one-time token spent on a run that then
	// stops at the link can't be used again.
	if err := refuseBadLink(opts, wd); err != nil {
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
	cfg, err := sessions.Config()
	if err != nil {
		return err
	}
	if err := refuseProjectOverride(opts, cfg, wd); err != nil {
		return err
	}
	// The directory is known now unless connect names it after a project.
	if dir := knownTarget(opts, wd); dir != "" {
		if err := checkStarter(dir, starter); err != nil {
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
	// The check before sign-in predicted the project from the flags and the
	// link. This one checks the project connect got.
	if err := projectOverrideError(authenticated.Config, project.Id.String()); err != nil {
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
	if err := waitUntilActive(ctx, authenticated.API, project); err != nil {
		return err
	}

	link, err := cliproject.Link(dir, project)
	if err != nil {
		return err
	}
	output.Success(opts.log, "Linked %s", relPath(dir, link.Path()))

	if err := scaffold(opts, dir, starter); err != nil {
		return err
	}

	anonKey, err := cliproject.DefaultAnonKey(ctx, authenticated.API, project.Id)
	if err != nil {
		return err
	}
	envPath := cliproject.CloudEnvPath(dir)
	if err := cliproject.WriteCloudEnv(dir, apiURL, anonKey); err != nil {
		return err
	}
	output.Success(opts.log, "Wrote the project's API URL and anon key to %s", cliproject.CloudEnvRelPath)

	if err := ensureGitignored(dir); err != nil {
		return err
	}

	dashboardURL := projectDashboardURL(authenticated.Config.WebURLForAPIURL(apiURL), project.Id)
	agentsPath := filepath.Join(dir, agentsFileName)
	// The shared AGENTS.md gets the dashboard's root, not this developer's project.
	dashboardRoot := strings.TrimRight(authenticated.Config.WebURLForAPIURL(apiURL), "/") + "/dashboard"
	if err := writeAgentsSection(agentsPath, agentsSection(dashboardRoot)); err != nil {
		return err
	}
	output.Success(opts.log, "Updated the Volcano section in %s", agentsFileName)

	skills := installSkills(ctx, opts)

	res := result{
		Project:      projectRef{ID: project.Id.String(), Name: project.Name},
		Created:      created,
		Directory:    dir,
		LinkFile:     link.Path(),
		EnvFile:      envPath,
		AgentsFile:   agentsPath,
		Skills:       skills,
		DashboardURL: dashboardURL,
	}
	if opts.jsonOutput {
		return output.JSON(opts.out, res)
	}
	shown := displayDir(wd, dir)
	fmt.Fprintln(opts.out)
	fmt.Fprintf(opts.out, "Project:   %s\n", project.Name)
	fmt.Fprintf(opts.out, "Directory: %s\n", shown)
	fmt.Fprintf(opts.out, "Dashboard: %s\n", dashboardURL)
	fmt.Fprintln(opts.out)
	if shown == "." {
		fmt.Fprintln(opts.out, "Next: read AGENTS.md and use the Volcano skills to build.")
	} else {
		fmt.Fprintf(opts.out, "Next: cd %s, read AGENTS.md there, and use the Volcano skills to build.\n", shown)
	}
	return nil
}

// ensureSession leaves a valid login saved: the --token given, the saved
// login if the API still accepts it, or a new browser login.
func ensureSession(ctx context.Context, opts options, sessions clisession.Factory) error {
	cfg, err := sessions.Config()
	if err != nil {
		return err
	}
	auth := cliauth.NewService(opts.deps)

	// A refused one-time token falls back to browser login, not to the saved
	// login: that may be another account, and the user who copied the prompt
	// expects the project in theirs.
	refused := false
	if token := strings.TrimSpace(opts.token); token != "" {
		// Only a project access token needs its project to sign in. Any other
		// token signs in first, so a wrong --project fails at the project
		// lookup with the login saved. LoginWithToken saves a spent one-time
		// token's login even when its own checks fail.
		project := ""
		if config.IsProjectToken(token) {
			project = opts.project
		}
		credentials, err := auth.LoginWithToken(ctx, cfg, token, project)
		if refusal, ok := errors.AsType[*cliauth.BootstrapRefusedError](err); ok {
			output.Warning(opts.log, "The one-time token %s. Signing in in the browser instead.", refusal.Reason)
			refused = true
		} else {
			if _, saved := errors.AsType[*cliauth.LoginSavedError](err); saved {
				output.Note(opts.log, "The one-time token is spent, but its login is saved. Run the command again without --token.")
			}
			if err != nil {
				return fmt.Errorf("token authentication failed: %w", err)
			}
			if err := cliauth.SaveCredentials(credentials); err != nil {
				return fmt.Errorf("failed to save credentials: %w", err)
			}
			output.Success(opts.log, "Signed in with the token")
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

	var credentials cliauth.Credentials
	if opts.signup {
		email := strings.TrimSpace(opts.email)
		if email == "" {
			email = cliauth.GitEmail(ctx, opts.deps)
		}
		if email == "" {
			return errors.New("--signup needs an email address: pass --email, or set git config --global user.email")
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
		if info, err := os.Stat(dir); err == nil && !info.IsDir() {
			return "", nil, false, fmt.Errorf("%s is not a directory", dir)
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
		return dir, project, created, makeDir(dir)
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

	taken := func(name string) bool { return exists(filepath.Join(wd, dirName(name))) }
	project, err := newProject(ctx, opts, session, generateName(), taken)
	if err != nil {
		return "", nil, false, err
	}
	dir := filepath.Join(wd, dirName(project.Name))
	return dir, project, true, makeDir(dir)
}

func makeDir(dir string) error {
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	return nil
}

// existingProject is the project --project names. It takes an ID only.
func existingProject(ctx context.Context, client *api.Client, raw string) (*apiclient.Project, error) {
	raw = strings.TrimSpace(raw)
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("--project takes a project ID, got %q. Run `volcano projects list` to find it", raw)
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

// refuseProjectOverride is projectOverrideError, checked before sign-in for
// the project connect will link: --project, else the project of a link connect
// keeps, else a new one. A one-time token spent on a run that then stops here
// can't be used again.
func refuseProjectOverride(opts options, cfg *config.Config, wd string) error {
	if raw := strings.TrimSpace(opts.project); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			// existingProject reports it.
			return nil //nolint:nilerr // not this check's error
		}
		return projectOverrideError(cfg, id.String())
	}
	start := wd
	if opts.dir != "" {
		dir, err := filepath.Abs(opts.dir)
		if err != nil {
			return err
		}
		start = dir
	}
	// A link connect can't use is refuseBadLink's, and means a new project.
	link, _ := config.FindLink(start)
	if link != nil && (opts.dir == "" || link.Dir == start) {
		return projectOverrideError(cfg, link.Project.ID)
	}
	return projectOverrideError(cfg, "")
}

// projectOverrideError refuses to link selectedID, or a new project when it is
// "", while VOLCANO_PROJECT_ID names another project: commands run in the
// directory would keep using that one, as cliproject.CheckProjectOverride
// explains for `volcano use`.
func projectOverrideError(cfg *config.Config, selectedID string) error {
	fromEnv, overrides := cliproject.EnvProjectOverride(cfg, selectedID)
	if !overrides {
		return nil
	}
	const prefix = "VOLCANO_PROJECT_ID is set to %s and would override the directory's link, so commands there would keep using that project. "
	if selectedID == "" {
		return fmt.Errorf(prefix+"Unset it, or pass --project %s to set up that project, and try again", fromEnv, fromEnv)
	}
	return fmt.Errorf(prefix+"Unset it, or set it to %s, and try again", fromEnv, selectedID)
}

func badLinkError(wd string, bad *config.LinkError) error {
	shown := displayDir(wd, bad.Dir)
	return fmt.Errorf("%w. To link %s to its project again, run: volcano connect %s --project <project-id>. "+
		"To link it to a new project, add --name <name>, or delete %s", bad, shown, shown, bad.Path())
}

// newProject creates a project named --name, else what the user types at a
// prompt, else fallback. A name taken() reports, or one the API says exists,
// gets a suffix.
func newProject(ctx context.Context, opts options, session *clisession.Session, fallback string, taken func(string) bool) (*apiclient.Project, error) {
	if err := session.Config.RequireAccountToken(); err != nil {
		return nil, fmt.Errorf("%w. To link the token's project, pass --project <project-id>", err)
	}
	// Checked before the project exists: VOLCANO_PROJECT_ID can't name it.
	if err := projectOverrideError(session.Config, ""); err != nil {
		return nil, err
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
			return nil, projectLimitError(ctx, client, err)
		default:
			return nil, fmt.Errorf("failed to create project %q: %w", candidate, err)
		}
	}
}

// projectName is --name, else what the user types at a prompt, else fallback.
// It asks only when stdin is a terminal and none of the environment markers
// the sign-in offer also checks (CI, VOLCANO_NONINTERACTIVE, TERM=dumb) is set.
func projectName(opts options, fallback string) (string, error) {
	if name := strings.TrimSpace(opts.name); name != "" {
		return name, nil
	}
	if opts.jsonOutput || confirm.MarkedNonInteractive() || !confirm.CanPrompt(opts.in) {
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
// project, and signup creates it.
func projectLimitError(ctx context.Context, client *api.Client, limitErr error) error {
	page, err := client.ListProjects(ctx, api.DefaultPage, api.DefaultLimit)
	if err != nil || len(page.Data) == 0 {
		return fmt.Errorf("%w\nDelete a project or upgrade your plan to create another", limitErr)
	}
	var list strings.Builder
	for _, p := range page.Data {
		fmt.Fprintf(&list, "\n  - %s: volcano connect --project %s", p.Name, p.Id)
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
func installSkills(ctx context.Context, opts options) []string {
	if opts.skipSkills {
		return nil
	}
	report, err := setup.Run(ctx, setup.Options{HTTPDoer: opts.deps.HTTPClient})
	if err != nil {
		output.Warning(opts.log, "Could not install the Volcano skills: %v. Run `volcano setup` to retry.", err)
		return nil
	}
	setup.RenderReport(opts.log, report)
	var installed []string
	for _, r := range report.Results {
		switch r.Status {
		case setup.StatusInstalled, setup.StatusUpdated, setup.StatusUpToDate:
			installed = append(installed, r.Harness)
		}
	}
	return installed
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
