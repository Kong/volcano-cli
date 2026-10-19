// Package auth wires the volcano login/logout/whoami commands.
package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"strings"

	"github.com/spf13/cobra"

	cliauth "github.com/Kong/volcano-cli/internal/auth"
	projectcmd "github.com/Kong/volcano-cli/internal/cmd/project"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/output"
	cliproject "github.com/Kong/volcano-cli/internal/project"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type loginOptions struct {
	deps      cliruntime.Deps
	token     string
	projectID string
	out       io.Writer
	errOut    io.Writer
}

type signupOptions struct {
	deps   cliruntime.Deps
	in     io.Reader
	out    io.Writer
	errOut io.Writer
}

// NewLogin returns the login command.
func NewLogin(deps cliruntime.Deps) *cobra.Command {
	var tokenFlag string
	var projectFlag string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Volcano",
		Long: `Authenticate with Volcano using browser-based login or a token.

Browser-based login (default):
  volcano login

Token-based login (for CI/CD):
  volcano login --token pk-xxxxxxxxxx

Project access tokens (pt-) reach one project only, so name it by ID:
  volcano login --token pt-xxxxxxxxxx --project <project-id>

Environment variable (no login needed):
  export VOLCANO_TOKEN=pk-xxxxxxxxxx
  volcano cloud functions deploy --all`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd.Context(), loginOptions{
				deps:      deps,
				token:     tokenFlag,
				projectID: projectFlag,
				out:       cmd.OutOrStdout(),
				errOut:    cmd.ErrOrStderr(),
			})
		},
	}
	cmd.Flags().StringVar(&tokenFlag, "token", "", "User token for authentication")
	cmd.Flags().StringVar(&projectFlag, "project", "",
		"Project ID or name to validate the token against and select (required for a pt- token, which must use the ID)")
	return cmd
}

func runLogin(ctx context.Context, opts loginOptions) error {
	cfg, err := loadConfig(opts.deps, opts.errOut)
	if err != nil {
		return err
	}

	service := cliauth.NewService(opts.deps)
	var credentials cliauth.Credentials
	token := strings.TrimSpace(opts.token)
	// --project inside a linked directory selects the project in the link, as
	// `volcano use` does there: the link would otherwise keep overriding it.
	inLink := token != "" && strings.TrimSpace(opts.projectID) != "" && cfg.Link != nil
	if token != "" {
		credentials, err = service.LoginWithToken(ctx, cfg, token, opts.projectID)
		if err == nil && inLink {
			// Checked before anything is saved, so a refusal changes nothing. A
			// one-time token's login is saved by now, so say that it is.
			if err = cliproject.CheckProjectOverride(cfg, credentials.Project.ID, cfg.Link); err != nil && config.IsBootstrapToken(token) {
				err = &cliauth.LoginSavedError{Err: err}
			}
		}
		if err != nil {
			if _, saved := errors.AsType[*cliauth.LoginSavedError](err); saved {
				output.Note(opts.out, "The one-time token is spent, but its login is saved, so you are logged in.")
				if strings.TrimSpace(opts.projectID) != "" {
					output.Note(opts.out, "Run `%s` to select a project.", cliruntime.CommandPath(opts.deps, "use <project>"))
				}
			}
			return fmt.Errorf("token authentication failed: %w", err)
		}
		fmt.Fprintln(opts.out)
		output.Success(opts.out, "Token validated")
	} else {
		if strings.TrimSpace(opts.projectID) != "" {
			return errors.New("--project applies to --token login; run 'volcano use <project-id>' after a browser login")
		}
		credentials, err = service.LoginWithBrowser(ctx, cfg, opts.out)
		if err != nil {
			return fmt.Errorf("browser authentication failed: %w", err)
		}
	}

	saved := credentials
	if inLink {
		// The link holds the selection. The global one stays as it is, as with
		// `volcano use`.
		saved.Project = nil
	}
	if err := cliauth.SaveCredentials(saved); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	output.Success(opts.out, "Logged in successfully")
	printNonDefaultAPI(opts.out, credentials)
	if credentials.Project == nil {
		output.Success(opts.out, "Credentials saved to ~/.volcano/config.json")
		return nil
	}
	output.Success(opts.out, "Now using project: %s (%s)", credentials.Project.Name, credentials.Project.ID)
	if !inLink {
		output.Success(opts.out, "Project saved to ~/.volcano/config.json")
		output.Success(opts.out, "Credentials saved to ~/.volcano/config.json")
		return nil
	}
	result, err := service.SelectInLink(ctx, cfg, credentials)
	if err != nil {
		return fmt.Errorf("logged in, but failed to select the project in this directory's link: %w", err)
	}
	output.Success(opts.out, "Project saved to %s", result.SavedTo)
	projectcmd.ReportCloudEnv(opts.deps, opts.out, opts.errOut, result)
	output.Success(opts.out, "Credentials saved to ~/.volcano/config.json")
	if previous := result.Previous; previous != nil {
		output.Warning(opts.errOut, "This directory was linked to %s. It is now linked to %s. To undo, run: %s",
			projectLabel(*previous), projectLabel(*credentials.Project), undoCommand(opts.deps, credentials, previous.ID))
	}
	return nil
}

// loadConfig loads the config for login and signup, which don't need the
// working directory's project link: a link that can't be used is a warning,
// not a failure.
func loadConfig(deps cliruntime.Deps, errOut io.Writer) (*config.Config, error) {
	cfg, linkErr, err := config.LoadIgnoringBadLink()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if linkErr != nil {
		output.Warning(errOut, "Ignoring a project link that can't be used: %v. Commands that use a project fail in this "+
			"directory until it is fixed. To relink it, run: %s", linkErr,
			cliruntime.CommandPath(deps, "connect "+linkErr.Dir+" --project <project-id>"))
	}
	return cfg, nil
}

// projectLabel is "name (id)", or the ID alone when the name is unknown.
func projectLabel(project config.ProjectConfig) string {
	if project.Name == "" {
		return project.ID
	}
	return project.Name + " (" + project.ID + ")"
}

// undoCommand links the directory back to projectID. A project access token
// reaches only its own project, so `volcano use` can't go back with it.
func undoCommand(deps cliruntime.Deps, credentials cliauth.Credentials, projectID string) string {
	if config.IsProjectToken(credentials.Token) {
		return cliruntime.CommandPath(deps, "login --token <token for that project> --project "+projectID)
	}
	return cliruntime.CommandPath(deps, "use "+projectID)
}

// NewSignup returns the signup command.
func NewSignup(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "signup",
		Short: "Create a Volcano account",
		Long: `Create a Volcano account from the CLI.

The command uses your git user.email as the default email address when available,
then opens Volcano's web signup flow in your browser. After you finish signing up,
the CLI completes authentication and saves your credentials, leaving you logged in.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSignup(cmd.Context(), signupOptions{
				deps:   deps,
				in:     cmd.InOrStdin(),
				out:    cmd.OutOrStdout(),
				errOut: cmd.ErrOrStderr(),
			})
		},
	}
}

func runSignup(ctx context.Context, opts signupOptions) error {
	cfg, err := loadConfig(opts.deps, opts.errOut)
	if err != nil {
		return err
	}

	reader := bufio.NewReader(opts.in)
	email, err := promptSignupEmail(ctx, opts.deps, reader, opts.out)
	if err != nil {
		return err
	}

	credentials, err := cliauth.NewService(opts.deps).Signup(ctx, cfg, email, opts.out)
	if err != nil {
		return fmt.Errorf("signup failed: %w", err)
	}

	if err := cliauth.SaveCredentials(credentials); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	output.Success(opts.out, "Signed up and logged in successfully")
	printNonDefaultAPI(opts.out, credentials)
	output.Success(opts.out, "Credentials saved to ~/.volcano/config.json")
	return nil
}

func promptSignupEmail(ctx context.Context, deps cliruntime.Deps, reader *bufio.Reader, out io.Writer) (string, error) {
	defaultEmail := cliauth.GitEmail(ctx, deps)
	if defaultEmail != "" {
		fmt.Fprintf(out, "Enter your email address (press enter to continue) [%s]: ", defaultEmail)
	} else {
		fmt.Fprint(out, "Enter your email address: ")
	}

	input, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	email := strings.TrimSpace(input)
	if email == "" {
		email = defaultEmail
	}
	if email == "" {
		return "", errors.New("email address is required")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil {
		return "", fmt.Errorf("invalid email address: %w", err)
	}
	return parsed.Address, nil
}

// printNonDefaultAPI names the API a login signed in to when it isn't the
// default. Later commands keep using that API, so the user should know.
func printNonDefaultAPI(w io.Writer, credentials cliauth.Credentials) {
	if credentials.APIURL == "" || strings.TrimRight(credentials.APIURL, "/") == strings.TrimRight(config.DefaultAPIURL(), "/") {
		return
	}
	output.Note(w, "Signed in to %s. Commands use this API until you log out or log in elsewhere.", credentials.APIURL)
}

// NewLogout returns the logout command.
func NewLogout() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Log out of Volcano",
		Long: `Log out by deleting local credentials from ~/.volcano/config.json.

This does not revoke the token - to fully revoke access, delete the token in the Volcano dashboard.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogout(cmd.OutOrStdout())
		},
	}
}

func runLogout(w io.Writer) error {
	if err := cliauth.NewService(cliruntime.Deps{}).Logout(); err != nil {
		return fmt.Errorf("failed to delete config: %w", err)
	}
	output.Success(w, "Logged out (deleted ~/.volcano/config.json)")
	return nil
}
