package domains

import (
	"context"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/verifieddomain"
)

type verifyOptions struct {
	deps       cliruntime.Deps
	domain     string
	jsonOutput bool
	out        io.Writer
}

func newVerify(deps cliruntime.Deps) *cobra.Command {
	opts := verifyOptions{}
	cmd := &cobra.Command{
		Use:   "verify <domain>",
		Short: "Verify that your account owns a domain",
		Long: `Verify that your account owns a domain and every name below it.

Verify a registrable domain such as example.com to cover all of its subdomains,
or a subdomain such as team.example.com when only that part of the zone is
yours.

Until DNS serves the domain's _volcano TXT record, this command fails and prints
the record to publish. Publish it, then run this command again. A domain another
account verified moves to yours once its record is no longer published.`,
		Example: `  volcano cloud domains verify example.com`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.deps = deps
			opts.domain = strings.TrimSpace(args[0])
			opts.out = cmd.OutOrStdout()
			return runVerify(cmd.Context(), opts)
		},
	}
	cmd.Flags().BoolVar(&opts.jsonOutput, "json", false, "Emit machine-readable JSON")
	return cmd
}

func runVerify(ctx context.Context, opts verifyOptions) error {
	domain, created, err := verifieddomain.NewService(opts.deps).Verify(ctx, opts.domain)
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return writeJSON(opts.out, domain)
	}
	if !created {
		output.Success(opts.out, "Domain '%s' is already verified", domain.Domain)
		return nil
	}
	output.Success(opts.out, "Domain '%s' verified", domain.Domain)
	output.Note(opts.out, "Attach %s or any name below it to your frontends. Keep the TXT record published.", domain.Domain)
	return nil
}
