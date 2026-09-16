package accesstokens

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	cliaccesstoken "github.com/Kong/volcano-cli/internal/accesstoken"
	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type revokeOptions struct {
	deps       cliruntime.Deps
	identifier string
	yes        bool
	in         io.Reader
	out        io.Writer
}

func newRevoke(deps cliruntime.Deps) *cobra.Command {
	opts := revokeOptions{}
	cmd := &cobra.Command{
		Use:   "revoke <name-or-id>",
		Short: "Revoke an access token",
		Long: `Revoke an access token for the current project.

The token stops working immediately. Its record is kept, with status "revoked",
so its usage history stays available.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.identifier = strings.TrimSpace(args[0])
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			return runRevoke(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runRevoke(ctx context.Context, opts revokeOptions) error {
	if !opts.yes {
		confirmed, err := confirm.Action(opts.in, opts.out,
			"Revoking a token immediately breaks every deployment, pipeline, and script still using it.",
			fmt.Sprintf("Revoke access token '%s'?", opts.identifier))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	token, err := cliaccesstoken.NewService(opts.deps).Revoke(ctx, opts.identifier)
	if err != nil {
		return err
	}

	output.Success(opts.out, "Access token '%s' revoked", token.Name)
	return nil
}
