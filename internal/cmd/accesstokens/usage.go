package accesstokens

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	cliaccesstoken "github.com/Kong/volcano-cli/internal/accesstoken"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type usageOptions struct {
	deps       cliruntime.Deps
	days       int
	jsonOutput bool
	out        io.Writer
}

func newUsage(deps cliruntime.Deps) *cobra.Command {
	opts := usageOptions{}
	cmd := &cobra.Command{
		Use:   "usage",
		Short: "Show recent request counts for every access token",
		Long: fmt.Sprintf(`Show how many requests each of the current project's access tokens
authenticated over the window, revoked tokens included.

"%s" prints one token's day-by-day series.

Examples:
  %s
  %s`,
			cliruntime.CommandPath(deps, "access-tokens get <name> --usage"),
			cliruntime.CommandPath(deps, "access-tokens usage"),
			cliruntime.CommandPath(deps, "access-tokens usage --days 7")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runUsage(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.days, "days", cliaccesstoken.DefaultUsageDays, "Days of usage to fetch (max 60)")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runUsage(ctx context.Context, opts usageOptions) error {
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
