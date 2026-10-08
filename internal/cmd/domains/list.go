package domains

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/verifieddomain"
)

type listOptions struct {
	deps       cliruntime.Deps
	jsonOutput bool
	out        io.Writer
}

func newList(deps cliruntime.Deps) *cobra.Command {
	opts := listOptions{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List verified domains",
		Long:  "List the domains your account has verified, including any Volcano reserves for its own account.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts.deps = deps
			opts.out = cmd.OutOrStdout()
			return runList(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runList(ctx context.Context, opts listOptions) error {
	domains, err := verifieddomain.NewService(opts.deps).List(ctx)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, domains)
	}
	output.VerifiedDomains(opts.out, domains)
	return nil
}
