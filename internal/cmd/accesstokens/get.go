package accesstokens

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliaccesstoken "github.com/Kong/volcano-cli/internal/accesstoken"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type getOptions struct {
	deps       cliruntime.Deps
	identifier string
	usage      bool
	days       int
	daysSet    bool
	jsonOutput bool
	out        io.Writer
}

func newGet(deps cliruntime.Deps) *cobra.Command {
	opts := getOptions{}
	cmd := &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get an access token",
		Long: fmt.Sprintf(`Show one access token's metadata, and with --usage its daily request counts.

Examples:
  %s
  %s`,
			cliruntime.CommandPath(deps, "access-tokens get ci-deploy"),
			cliruntime.CommandPath(deps, "access-tokens get ci-deploy --usage --days 7")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.identifier = strings.TrimSpace(args[0])
			opts.daysSet = cmd.Flags().Changed("days")
			opts.out = cmd.OutOrStdout()
			return runGet(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.usage, "usage", false, "Also fetch the token's daily request counts")
	cmd.Flags().IntVar(&opts.days, "days", cliaccesstoken.DefaultUsageDays, "Days of usage to fetch with --usage (max 60)")
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runGet(ctx context.Context, opts getOptions) error {
	if opts.daysSet && !opts.usage {
		return errors.New("--days applies to --usage; pass --usage to fetch the request counts")
	}
	if opts.usage {
		if err := cliaccesstoken.ValidateUsageDays(opts.days); err != nil {
			return err
		}
	}

	service := cliaccesstoken.NewService(opts.deps)
	token, err := service.Get(ctx, opts.identifier)
	if err != nil {
		return err
	}

	var usage *apiclient.ProjectAccessTokenUsage
	if opts.usage {
		usage, err = service.Usage(ctx, opts.identifier, opts.days)
		if err != nil {
			return err
		}
	}

	if opts.jsonOutput {
		if usage != nil {
			return writeJSON(opts.out, map[string]any{"token": token, "usage": usage})
		}
		return writeJSON(opts.out, token)
	}

	output.AccessToken(opts.out, token)
	if usage != nil {
		output.AccessTokenUsage(opts.out, usage)
	}
	return nil
}
