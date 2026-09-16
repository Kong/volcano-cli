package accesstokens

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliaccesstoken "github.com/Kong/volcano-cli/internal/accesstoken"
	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type createOptions struct {
	deps      cliruntime.Deps
	name      string
	nameFlag  string
	scope     string
	expiresAt string
	out       io.Writer
}

func newCreate(deps cliruntime.Deps) *cobra.Command {
	opts := createOptions{}
	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a project access token",
		Long: fmt.Sprintf(`Mint an access token for the current cloud project.

The token secret is printed once, on creation, and cannot be retrieved again.

Examples:
  %s
  %s`,
			cliruntime.CommandPath(deps, "access-tokens create ci-deploy"),
			cliruntime.CommandPath(deps, "access-tokens create ci-audit --scope read_only --expires-at 2027-01-31T00:00:00Z")),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			if len(args) == 1 {
				opts.name = strings.TrimSpace(args[0])
			}
			opts.out = cmd.OutOrStdout()
			return runCreate(cmd.Context(), opts)
		},
	}
	cmd.Flags().StringVar(&opts.nameFlag, "name", "", "Token name, when not given as an argument")
	cmd.Flags().StringVar(&opts.scope, "scope", cliaccesstoken.ScopeFull,
		fmt.Sprintf("Token scope (%s)", strings.Join(cliaccesstoken.Scopes(), " or ")))
	cmd.Flags().StringVar(&opts.expiresAt, "expires-at", "", "Expiry as an RFC3339 timestamp (default: never expires)")
	return cmd
}

func runCreate(ctx context.Context, opts createOptions) error {
	name := strings.TrimSpace(opts.name)
	nameFlag := strings.TrimSpace(opts.nameFlag)
	switch {
	case name != "" && nameFlag != "":
		return errors.New("specify the token name once, as an argument or --name, not both")
	case name == "" && nameFlag == "":
		return errors.New("specify a token name as an argument or --name")
	case name == "":
		name = nameFlag
	}

	if err := cliaccesstoken.ValidateScope(opts.scope); err != nil {
		return err
	}
	expiresAt, err := cliaccesstoken.ParseExpiry(opts.expiresAt)
	if err != nil {
		return err
	}

	token, err := cliaccesstoken.NewService(opts.deps).Create(ctx, api.AccessTokenCreateInput{
		Name:      name,
		Scope:     strings.TrimSpace(opts.scope),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return err
	}

	output.AccessTokenCreated(opts.out, token)
	return nil
}
