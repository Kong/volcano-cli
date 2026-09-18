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
	service := cliaccesstoken.NewService(opts.deps)
	// Resolved before the prompt so the question names the token that will be
	// revoked: a mistyped name is reported as missing instead of after the user
	// has confirmed it, and a UUID argument is confirmed by name and prefix
	// rather than by the UUID the user already typed.
	token, err := service.Get(ctx, opts.identifier)
	if err != nil {
		return err
	}

	if !opts.yes {
		const warning = "Revoking a token immediately breaks every deployment, pipeline, and script still using it."
		// A prompt nobody can answer reads as a decline, so without this the
		// command exits 0 with the token still live — and the caller most likely
		// to hit it is the incident script revoking a leaked credential, which
		// has no way to tell that apart from a revocation that worked.
		if !confirm.CanPrompt(opts.in) {
			return fmt.Errorf("%s\n\nstdin is not a terminal: confirmation required; pass --yes", warning)
		}
		confirmed, err := confirm.Action(opts.in, opts.out, warning,
			fmt.Sprintf("Revoke access token '%s' (%s)?", token.Name, token.TokenPrefix))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := service.Revoke(ctx, token); err != nil {
		return err
	}

	output.Success(opts.out, "Access token '%s' revoked", token.Name)
	return nil
}
