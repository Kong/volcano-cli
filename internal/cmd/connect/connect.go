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
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	// cloudEnvRelPath is the env file cloud deploys read, as `volcano init`
	// names it.
	cloudEnvRelPath = "volcano/cloud.env"
	agentsFileName  = "AGENTS.md"
	// projectReadyTimeout bounds the wait for a new project to become active.
	projectReadyTimeout = 2 * time.Minute
	projectPollInterval = 2 * time.Second

	dirMode  fs.FileMode = 0o755
	fileMode fs.FileMode = 0o644
	// envFileMode is owner-only: the env file holds keys.
	envFileMode fs.FileMode = 0o600
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
	Skills       string     `json:"skills,omitempty"`
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
		Long: `Sign in, create a cloud project named after the directory, and link the
directory to it, ready for a coding agent to build on. Safe to run again.

connect:
  1. Signs in: reuses a valid login, takes --token, or runs browser login.
  2. Creates a project named after the directory (or --name). With --project it
     links an existing project instead. If your plan's project limit is
     reached, it links your existing project.
  3. Links the directory in .volcano/project.json. Commands run inside the
     directory use this project.
  4. Creates the volcano/ scaffold if it is missing (or --template's starter).
  5. Writes the project's API URL and anon key to volcano/cloud.env.
  6. Gitignores the link and the env files, and adds a Volcano section to
     AGENTS.md.
  7. Installs the Volcano skills for the coding agent running it.

With a directory argument, connect creates it and works there.

  volcano connect                          Link this directory
  volcano connect my-app                   Create ./my-app and link it
  volcano connect --project my-existing    Link an existing project
  volcano connect --signup                 New to Volcano: create an account first
  npx -y @volcano.dev/cli connect          No install needed`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.dir = "."
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
	cmd.Flags().StringVar(&opts.token, "token", "", "Sign in with an account token (pk-) instead of the browser")
	cmd.Flags().StringVar(&opts.project, "project", "", "Link this existing project (ID or name) instead of creating one")
	cmd.Flags().StringVar(&opts.name, "name", "", "Name for the new project (default: the directory name)")
	cmd.Flags().StringVar(&opts.template, "template", "", "Starter to scaffold: nextjs, javascript, python, ruby (default: base scaffold)")
	cmd.Flags().BoolVar(&opts.signup, "signup", false, "Create a Volcano account in the browser before connecting")
	cmd.Flags().StringVar(&opts.email, "email", "", "Email for --signup (default: git config user.email)")
	cmd.Flags().BoolVar(&opts.skipSkills, "skip-skills", false, "Don't install the Volcano skills for the calling agent")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Print the result as JSON")
	cmd.MarkFlagsMutuallyExclusive("token", "signup")
	cmd.MarkFlagsMutuallyExclusive("project", "name")
	return cmd
}

func run(ctx context.Context, opts options) error {
	dir, err := filepath.Abs(opts.dir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	starter, err := initcmd.ResolveStarter(opts.template, "")
	if err != nil {
		return err
	}

	sessions := clisession.NewFactory(opts.deps)
	if err := ensureSession(ctx, opts, sessions); err != nil {
		return err
	}
	authenticated, err := sessions.Authenticated()
	if err != nil {
		return err
	}
	apiURL := sessions.APIURL(authenticated.Config)

	project, created, err := pickProject(ctx, opts, authenticated, dir)
	if err != nil {
		return err
	}
	if created {
		output.Success(opts.log, "Created project %s (%s)", project.Name, project.Id)
	} else {
		output.Success(opts.log, "Using project %s (%s)", project.Name, project.Id)
	}
	if err := waitUntilActive(ctx, authenticated.API, project); err != nil {
		return err
	}

	link, err := cliproject.Link(dir, project)
	if err != nil {
		return err
	}
	output.Success(opts.log, "Linked %s", relPath(dir, link.Path()))

	scaffold(opts.log, dir, starter)

	anonKey, err := projectAnonKey(ctx, authenticated.API, project.Id)
	if err != nil {
		return err
	}
	envPath := filepath.Join(dir, filepath.FromSlash(cloudEnvRelPath))
	if err := upsertEnv(envPath, []envVar{
		{"VOLCANO_API_URL", apiURL},
		{"VOLCANO_ANON_KEY", anonKey},
		{"NEXT_PUBLIC_VOLCANO_API_URL", apiURL},
		{"NEXT_PUBLIC_VOLCANO_ANON_KEY", anonKey},
	}); err != nil {
		return err
	}
	output.Success(opts.log, "Wrote the project's API URL and anon key to %s", cloudEnvRelPath)

	if err := ensureGitignored(dir); err != nil {
		return err
	}

	dashboardURL := strings.TrimRight(authenticated.Config.WebURLForAPIURL(apiURL), "/") + "/dashboard/overview"
	agentsPath := filepath.Join(dir, agentsFileName)
	if err := writeAgentsSection(agentsPath, agentsSection(project, dashboardURL)); err != nil {
		return err
	}
	output.Success(opts.log, "Added the Volcano section to %s", agentsFileName)

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
	fmt.Fprintln(opts.out)
	fmt.Fprintf(opts.out, "Dashboard: %s (project %s)\n", dashboardURL, project.Name)
	fmt.Fprintln(opts.out, "Next: read AGENTS.md and use the Volcano skills to build.")
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

	if token := strings.TrimSpace(opts.token); token != "" {
		credentials, err := auth.LoginWithToken(ctx, cfg, token, opts.project)
		if err != nil {
			return fmt.Errorf("token authentication failed: %w", err)
		}
		if err := cliauth.SaveCredentials(credentials); err != nil {
			return fmt.Errorf("failed to save credentials: %w", err)
		}
		output.Success(opts.log, "Signed in with the token")
		return nil
	}

	if token := cfg.Token(); token != "" && !opts.signup {
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

// pickProject returns the project to link and whether connect created it:
// --project, else the directory's existing link, else a new project.
func pickProject(ctx context.Context, opts options, session *clisession.Session, dir string) (*apiclient.Project, bool, error) {
	client := session.API
	if identifier := strings.TrimSpace(opts.project); identifier != "" {
		project, err := findProject(ctx, session, identifier)
		return project, false, err
	}

	link, err := config.FindLink(dir)
	if err != nil {
		return nil, false, err
	}
	if link != nil && link.Dir == dir {
		id, parseErr := uuid.Parse(link.Project.ID)
		if parseErr != nil {
			return nil, false, fmt.Errorf("project link %s has an invalid project ID %q", link.Path(), link.Project.ID)
		}
		project, err := client.GetProject(ctx, id)
		if err == nil {
			return project, false, nil
		}
		if api.Status(err) != http.StatusNotFound {
			return nil, false, fmt.Errorf("failed to get the linked project: %w", err)
		}
		output.Warning(opts.log, "The linked project %s no longer exists. Creating a new one.", link.Project.ID)
	}

	if err := session.Config.RequireAccountToken(); err != nil {
		return nil, false, fmt.Errorf("%w. To link the token's project, pass --project <project-id>", err)
	}
	name, err := projectName(opts, dir)
	if err != nil {
		return nil, false, err
	}
	project, err := client.CreateProject(ctx, name, "")
	if err == nil {
		return project, true, nil
	}
	switch {
	case api.Status(err) == http.StatusConflict:
		// Ours already, from an earlier run or by hand: link it.
		project, findErr := cliproject.Find(ctx, client, name)
		if findErr != nil {
			return nil, false, fmt.Errorf("failed to create project %q: %w", name, err)
		}
		return project, false, nil
	case isProjectLimit(err):
		project, err := existingProjectAtLimit(ctx, opts, client, err)
		return project, false, err
	default:
		return nil, false, fmt.Errorf("failed to create project %q: %w", name, err)
	}
}

func findProject(ctx context.Context, session *clisession.Session, identifier string) (*apiclient.Project, error) {
	if id, err := uuid.Parse(identifier); err == nil {
		project, err := session.API.GetProject(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("failed to get project %s: %w", identifier, err)
		}
		return project, nil
	}
	if err := session.Config.RequireAccountToken(); err != nil {
		return nil, err
	}
	return cliproject.Find(ctx, session.API, identifier)
}

// projectName is --name, else what the user types at a prompt, else the
// directory's name made safe for a project name.
func projectName(opts options, dir string) (string, error) {
	if name := strings.TrimSpace(opts.name); name != "" {
		return name, nil
	}
	fallback := defaultProjectName(dir)
	if opts.jsonOutput || !confirm.CanPrompt(opts.in) {
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

func defaultProjectName(dir string) string {
	name := unsafeNameChars.ReplaceAllString(strings.ToLower(filepath.Base(dir)), "-")
	name = strings.Trim(name, "-")
	if len(name) < 2 {
		return "my-app"
	}
	return name
}

func isProjectLimit(err error) bool {
	return api.Status(err) == http.StatusForbidden && strings.Contains(strings.ToLower(api.Message(err)), "project limit")
}

// existingProjectAtLimit picks an existing project when the plan allows no
// more. The free plan allows one, and signup creates it, so a new user always
// lands here.
func existingProjectAtLimit(ctx context.Context, opts options, client *api.Client, limitErr error) (*apiclient.Project, error) {
	page, err := client.ListProjects(ctx, api.DefaultPage, api.DefaultLimit)
	if err != nil {
		return nil, fmt.Errorf("failed to list projects: %w", err)
	}
	projects := page.Data
	switch {
	case len(projects) == 1:
		output.Note(opts.log, "Your plan's project limit is reached, so connect links your existing project.")
		return &projects[0], nil
	case len(projects) > 1 && !opts.jsonOutput && confirm.CanPrompt(opts.in):
		return chooseProject(opts, projects)
	}
	var list strings.Builder
	for _, p := range projects {
		fmt.Fprintf(&list, "\n  - %s (%s)", p.Name, p.Id)
	}
	return nil, fmt.Errorf("%w\nPass --project with one of your projects:%s", limitErr, list.String())
}

func chooseProject(opts options, projects []apiclient.Project) (*apiclient.Project, error) {
	fmt.Fprintln(opts.log, "Your plan's project limit is reached. Link an existing project:")
	for i, p := range projects {
		fmt.Fprintf(opts.log, "  %d. %s (%s)\n", i+1, p.Name, p.Id)
	}
	fmt.Fprint(opts.log, "Number: ")
	line, err := bufio.NewReader(opts.in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(projects) {
		return nil, fmt.Errorf("invalid choice %q", strings.TrimSpace(line))
	}
	return &projects[n-1], nil
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

// scaffold creates the volcano/ scaffold when it is missing, or the starter
// --template names. A rerun leaves an existing scaffold alone: init treats
// files the user changed as conflicts.
func scaffold(log io.Writer, dir, starter string) {
	if starter == "" && exists(filepath.Join(dir, "volcano")) {
		return
	}
	result, err := projectinit.RunStarterIn(dir, starter, false)
	if err != nil {
		if conflicts, ok := projectinit.ConflictMessages(err); ok {
			output.Warning(log, "Skipped the scaffold: %s. Run `volcano init` to see the conflicts.", strings.Join(conflicts, "; "))
			return
		}
		output.Warning(log, "Skipped the scaffold: %v", err)
		return
	}
	if created := result.Created(); len(created) > 0 {
		output.Success(log, "Created the scaffold (%d files)", len(created))
	}
}

// projectAnonKey returns the project's default anon key, the publishable key
// the SDK and frontends use. It creates one only when the project has none.
func projectAnonKey(ctx context.Context, client *api.Client, projectID uuid.UUID) (string, error) {
	keys, err := client.ListAnonKeys(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("failed to get the project's anon key: %w", err)
	}
	var fallback string
	for _, key := range keys {
		if key.KeyValue == "" {
			continue
		}
		if key.IsDefault != nil && *key.IsDefault {
			return key.KeyValue, nil
		}
		if fallback == "" {
			fallback = key.KeyValue
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	key, err := client.CreateAnonKey(ctx, projectID, "default")
	if err != nil {
		return "", fmt.Errorf("failed to create an anon key: %w", err)
	}
	return key.KeyValue, nil
}

// installSkills installs the Volcano skills for the agent running connect and
// returns its name, or "" when none was installed. With no calling agent it
// only points at `volcano setup`, which asks before installing anywhere.
func installSkills(ctx context.Context, opts options) string {
	if opts.skipSkills {
		return ""
	}
	agent := setup.CallingAgent(os.Getenv)
	if agent == "" {
		fmt.Fprintln(opts.log, "To install the Volcano skills for your coding agents, run `volcano setup`.")
		return ""
	}
	report, err := setup.Run(ctx, setup.Options{HTTPDoer: opts.deps.HTTPClient, Only: []string{agent}, BestEffort: true})
	if err != nil {
		output.Warning(opts.log, "Could not install the Volcano skills for %s: %v", agent, err)
		return ""
	}
	setup.RenderReport(opts.log, report)
	for _, r := range report.Results {
		switch r.Status {
		case setup.StatusInstalled, setup.StatusUpdated, setup.StatusUpToDate:
			return agent
		}
	}
	return ""
}

func relPath(base, path string) string {
	if rel, err := filepath.Rel(base, path); err == nil {
		return rel
	}
	return path
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
