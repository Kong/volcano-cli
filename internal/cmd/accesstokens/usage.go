package accesstokens

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	cliaccesstoken "github.com/Kong/volcano-cli/internal/accesstoken"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type usageOptions struct {
	deps       cliruntime.Deps
	tokenID    string
	days       int
	jsonOutput bool
	out        io.Writer
}

func newUsage(deps cliruntime.Deps) *cobra.Command {
	opts := usageOptions{}
	cmd := &cobra.Command{
		Use:   "usage [token-id]",
		Short: "Show recent request counts for the project's access tokens",
		Long: fmt.Sprintf(`Show how many requests each of the current project's access tokens
authenticated over the window, revoked tokens included.

Given a token ID, show that token's day-by-day series instead. Both forms run
with a project access token, so a CI job can report what it consumed with
nothing but the credential it already holds. They take an ID rather than a name
because turning a name into an ID needs an account token, which is also what
"%s" needs.

Examples:
  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "access-tokens get <name> --usage"),
			cliruntime.CommandPath(deps, "access-tokens usage"),
			cliruntime.CommandPath(deps, "access-tokens usage --days 7"),
			cliruntime.CommandPath(deps, "access-tokens usage 7f1c2e94-2a6b-4c17-9a42-1b0c8f5d3e77")),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			if len(args) == 1 {
				opts.tokenID = strings.TrimSpace(args[0])
			}
			opts.out = cmd.OutOrStdout()
			return runUsage(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.days, "days", cliaccesstoken.DefaultUsageDays, "Days of usage to fetch (max 60)")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runUsage(ctx context.Context, opts usageOptions) error {
	if err := cliaccesstoken.ValidateUsageDays(opts.days); err != nil {
		return err
	}
	if opts.tokenID != "" {
		return runTokenUsage(ctx, opts)
	}

	usage, err := cliaccesstoken.NewService(opts.deps).ProjectUsage(ctx, opts.days)
	if err != nil {
		return err
	}

	if opts.jsonOutput {
		return writeJSON(opts.out, usage)
	}

	output.AccessTokensUsage(opts.out, usage)
	return nil
}

func runTokenUsage(ctx context.Context, opts usageOptions) error {
	tokenID, err := uuid.Parse(opts.tokenID)
	if err != nil {
		return fmt.Errorf("invalid token ID %q: expected a UUID. A name has to be resolved first, which needs an "+
			"account token — run `%s`", opts.tokenID,
			cliruntime.CommandPath(opts.deps, "access-tokens get "+opts.tokenID+" --usage"))
	}

	usage, err := cliaccesstoken.NewService(opts.deps).TokenUsage(ctx, tokenID, opts.days)
	if err != nil {
		return err
	}

	if opts.jsonOutput {
		return writeJSON(opts.out, usage)
	}

	output.AccessTokenUsage(opts.out, usage)
	return nil
}
