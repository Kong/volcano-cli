// Package auth wires the volcano login/logout/whoami commands.
package auth

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	cliauth "github.com/Kong/volcano-cli/internal/auth"
	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type loginOptions struct {
	deps  cliruntime.Deps
	token string
	out   io.Writer
}

type signupOptions struct {
	deps cliruntime.Deps
	in   io.Reader
	out  io.Writer
}

// NewLogin returns the login command.
func NewLogin(deps cliruntime.Deps) *cobra.Command {
	var tokenFlag string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with Volcano",
		Long: `Authenticate with Volcano using browser-based login or a token.

Browser-based login (default):
  volcano login

Token-based login (for CI/CD):
  volcano login --token pk-xxxxxxxxxx

Environment variable (no login needed):
  export VOLCANO_TOKEN=pk-xxxxxxxxxx
  volcano cloud functions deploy --all`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(cmd.Context(), loginOptions{
				deps:  deps,
				token: tokenFlag,
				out:   cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&tokenFlag, "token", "", "User token for authentication")
	return cmd
}

func runLogin(ctx context.Context, opts loginOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	service := cliauth.NewService(opts.deps)
	var credentials cliauth.Credentials
	token := strings.TrimSpace(opts.token)
	if token != "" {
		credentials, err = service.LoginWithToken(ctx, cfg, token)
		if err != nil {
			return fmt.Errorf("token authentication failed: %w", err)
		}
		fmt.Fprintln(opts.out)
		output.Success(opts.out, "Token validated")
	} else {
		credentials, err = service.LoginWithBrowser(ctx, cfg, opts.out)
		if err != nil {
			return fmt.Errorf("browser authentication failed: %w", err)
		}
	}

	cfg.UserToken = credentials.Token
	cfg.UserID = credentials.UserID
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save credentials: %w", err)
	}

	output.Success(opts.out, "Logged in successfully")
	output.Success(opts.out, "Credentials saved to ~/.volcano/config.json")
	return nil
}

// NewSignup returns the signup command.
func NewSignup(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "signup",
		Short: "Create a Volcano account",
		Long: `Create a Volcano account from the CLI.

The command uses your git user.email as the default email address when available,
then opens Volcano's web signup flow in your browser.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSignup(cmd.Context(), signupOptions{
				deps: deps,
				in:   cmd.InOrStdin(),
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func runSignup(ctx context.Context, opts signupOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	reader := bufio.NewReader(opts.in)
	email, err := promptSignupEmail(ctx, opts.deps, reader, opts.out)
	if err != nil {
		return err
	}

	if err := cliauth.NewService(opts.deps).Signup(ctx, cfg, email, opts.out); err != nil {
		return fmt.Errorf("signup failed: %w", err)
	}

	fmt.Fprint(opts.out, "\nComplete signup in your browser, then press [ENTER] when done.")
	_, err = reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func promptSignupEmail(ctx context.Context, deps cliruntime.Deps, reader *bufio.Reader, out io.Writer) (string, error) {
	defaultEmail := gitConfigEmail(ctx, deps)
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

func gitConfigEmail(ctx context.Context, deps cliruntime.Deps) string {
	runner := deps.GitCommandRunner
	if runner == nil {
		runner = cliruntime.CommandRunnerFunc(func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // command name and args are static below
		})
	}
	out, err := runner.Run(ctx, "git", "config", "--global", "user.email")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
