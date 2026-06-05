// Package auth wires the volcano login/logout/whoami commands.
package auth

import (
	"context"
	"fmt"
	"io"
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
  volcano functions deploy --all`,
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
