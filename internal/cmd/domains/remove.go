package domains

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/confirm"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/verifieddomain"
)

type removeOptions struct {
	deps   cliruntime.Deps
	domain string
	yes    bool
	in     io.Reader
	out    io.Writer
}

func newRemove(deps cliruntime.Deps) *cobra.Command {
	opts := removeOptions{}
	cmd := &cobra.Command{
		Use:   "remove <domain>",
		Short: "Remove a verified domain",
		Long: `Give up your account's ownership of a domain.

Custom domains already attached below it keep serving. Attaching another one
needs the domain verified again.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.domain = strings.TrimSpace(args[0])
			opts.in = cmd.InOrStdin()
			opts.out = cmd.OutOrStdout()
			return runRemove(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runRemove(ctx context.Context, opts removeOptions) error {
	if !opts.yes {
		const warning = "Attaching new custom domains below it will need the domain verified again."
		if !confirm.CanPrompt(opts.in) {
			return fmt.Errorf("%s\n\nstdin is not a terminal: confirmation required; pass --yes", warning)
		}
		confirmed, err := confirm.Action(opts.in, opts.out, warning, fmt.Sprintf("Remove verified domain '%s'?", opts.domain))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}
	if err := verifieddomain.NewService(opts.deps).Remove(ctx, opts.domain); err != nil {
		return err
	}
	output.Success(opts.out, "Domain '%s' removed", opts.domain)
	return nil
}
