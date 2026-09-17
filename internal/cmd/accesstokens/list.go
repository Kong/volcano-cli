package accesstokens

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	cliaccesstoken "github.com/Kong/volcano-cli/internal/accesstoken"
	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type listOptions struct {
	deps           cliruntime.Deps
	page           int
	limit          int
	search         string
	includeRevoked bool
	jsonOutput     bool
	out            io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List access tokens",
		Long: "List the current project's access tokens. Tokens that can no longer authenticate, " +
			"revoked and expired alike, are hidden unless --include-revoked is set.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runList(cmd.Context(), opts)
		},
	}
	cmd.Flags().IntVar(&opts.page, "page", api.DefaultPage, "Page number to fetch")
	cmd.Flags().IntVar(&opts.limit, "limit", api.DefaultLimit, "Number of access tokens per page")
	cmd.Flags().StringVar(&opts.search, "search", "", "Filter by a substring of the token name")
	cmd.Flags().BoolVar(&opts.includeRevoked, "include-revoked", false, "Include revoked and expired tokens")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	if err := cliaccesstoken.ValidateListWindow(opts.page, opts.limit); err != nil {
		return err
	}

	tokens, err := cliaccesstoken.NewService(opts.deps).ListPage(ctx, api.AccessTokenListInput{
		Page:           opts.page,
		Limit:          opts.limit,
		Search:         opts.search,
		IncludeRevoked: opts.includeRevoked,
	})
	if err != nil {
		return err
	}

	if opts.jsonOutput {
		return writeJSON(opts.out, tokens)
	}

	output.AccessTokens(opts.out, tokens, cliruntime.CommandPath(opts.deps, ""))
	return nil
}
