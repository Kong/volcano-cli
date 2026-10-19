package frontends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/confirm"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

var redirectStatuses = []int{301, 302, 307, 308}

type domainsListOptions struct {
	deps     cliruntime.Deps
	frontend string
	out      io.Writer
}

type domainsGetOptions struct {
	deps     cliruntime.Deps
	frontend string
	domain   string
	out      io.Writer
}

type domainsAddOptions struct {
	deps      cliruntime.Deps
	frontend  string
	input     api.FrontendDomainInput
	certPath  string
	keyPath   string
	chainPath string
	out       io.Writer
}

type domainsUpdateOptions struct {
	deps     cliruntime.Deps
	frontend string
	domain   string
	redirect *api.FrontendDomainRedirect
	out      io.Writer
}

type domainsRemoveOptions struct {
	deps     cliruntime.Deps
	frontend string
	domain   string
	yes      bool
	in       io.Reader
	out      io.Writer
}

// redirectFlags are --redirect-to and --redirect-status, shared by add and
// update.
type redirectFlags struct {
	to     string
	status int
}

func (f *redirectFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.to, "redirect-to", "", "Redirect every request to this domain of the frontend, which must serve it")
	cmd.Flags().IntVar(&f.status, "redirect-status", 308, "Redirect status: 301, 302, 307, or 308")
}

// validate refuses what the API would answer with a bare "invalid request".
func (f *redirectFlags) validate(cmd *cobra.Command) error {
	switch {
	case cmd.Flags().Changed("redirect-status") && !cmd.Flags().Changed("redirect-to"):
		return errors.New("--redirect-status needs --redirect-to")
	case !slices.Contains(redirectStatuses, f.status):
		return fmt.Errorf("invalid --redirect-status %d: use 301, 302, 307, or 308", f.status)
	default:
		return nil
	}
}

func (f *redirectFlags) redirect(cmd *cobra.Command) *api.FrontendDomainRedirect {
	if !cmd.Flags().Changed("redirect-to") {
		return nil
	}
	return &api.FrontendDomainRedirect{To: strings.TrimSpace(f.to), Status: f.status}
}

func newDomains(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domains",
		Short: "Manage a frontend's custom domains",
		Long: fmt.Sprintf(`Manage the custom domains of a frontend.

A frontend can have as many custom domains as its plan allows. Each one either
serves the frontend or redirects every request, keeping the path and query
string, to another domain of the same frontend that serves it, such as
example.com to www.example.com.

Every domain must sit at or below a domain your account has verified with
'%s'.`, cliruntime.CommandPath(deps, "domains verify")),
	}
	cmd.AddCommand(newDomainsList(deps))
	cmd.AddCommand(newDomainsGet(deps))
	cmd.AddCommand(newDomainsAdd(deps))
	cmd.AddCommand(newDomainsUpdate(deps))
	cmd.AddCommand(newDomainsRemove(deps))
	return cmd
}

func newDomainsList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list <frontend>",
		Short: "List a frontend's custom domains",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainsList(cmd.Context(), domainsListOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func newDomainsGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <frontend> <domain>",
		Short: "Show one custom domain of a frontend",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainsGet(cmd.Context(), domainsGetOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				domain:   strings.TrimSpace(args[1]),
				out:      cmd.OutOrStdout(),
			})
		},
	}
}

func newDomainsAdd(deps cliruntime.Deps) *cobra.Command {
	var redirect redirectFlags
	var certPath, keyPath, chainPath string
	cmd := &cobra.Command{
		Use:   "add <frontend> <domain>",
		Short: "Add a custom domain to a frontend",
		Long: `Add a custom domain beside the frontend's others.

Without --cert and --key, Volcano issues and renews the certificate once you
publish the DNS records this command prints. With them, the domain serves your
certificate.

With --redirect-to, the domain redirects every request to another domain of the
frontend instead of serving it. It still needs a certificate and DNS pointing
at the frontend, because browsers connect over HTTPS before they read the
redirect.`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "frontends domains add web www.example.com"),
			cliruntime.CommandPath(deps, "frontends domains add web example.com --redirect-to www.example.com"),
			cliruntime.CommandPath(deps, "frontends domains add web app.example.com --cert fullchain.pem --key privkey.pem")),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := redirect.validate(cmd); err != nil {
				return err
			}
			if chainPath != "" && certPath == "" {
				return errors.New("--chain needs --cert and --key")
			}
			return runDomainsAdd(cmd.Context(), domainsAddOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				input: api.FrontendDomainInput{
					Domain:   strings.TrimSpace(args[1]),
					Redirect: redirect.redirect(cmd),
				},
				certPath:  certPath,
				keyPath:   keyPath,
				chainPath: chainPath,
				out:       cmd.OutOrStdout(),
			})
		},
	}
	redirect.register(cmd)
	cmd.Flags().StringVar(&certPath, "cert", "", "Path to a PEM-encoded certificate, to serve your own instead of a Volcano-issued one")
	cmd.Flags().StringVar(&keyPath, "key", "", "Path to the certificate's PEM-encoded private key")
	cmd.Flags().StringVar(&chainPath, "chain", "", "Path to a PEM-encoded certificate chain")
	cmd.MarkFlagsRequiredTogether("cert", "key")
	return cmd
}

func newDomainsUpdate(deps cliruntime.Deps) *cobra.Command {
	var redirect redirectFlags
	var noRedirect bool
	cmd := &cobra.Command{
		Use:   "update <frontend> <domain>",
		Short: "Make a custom domain redirect, or serve the frontend again",
		Long: `Set or clear a custom domain's redirect.

--redirect-to makes the domain redirect every request to another domain of the
frontend that serves it. --no-redirect makes it serve the frontend again. A
domain that other domains redirect to cannot become a redirect itself.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "frontends domains update web example.com --redirect-to www.example.com --redirect-status 302"),
			cliruntime.CommandPath(deps, "frontends domains update web example.com --no-redirect")),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := redirect.validate(cmd); err != nil {
				return err
			}
			if !noRedirect && !cmd.Flags().Changed("redirect-to") {
				return errors.New("specify --redirect-to or --no-redirect")
			}
			return runDomainsUpdate(cmd.Context(), domainsUpdateOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				domain:   strings.TrimSpace(args[1]),
				redirect: redirect.redirect(cmd),
				out:      cmd.OutOrStdout(),
			})
		},
	}
	redirect.register(cmd)
	cmd.Flags().BoolVar(&noRedirect, "no-redirect", false, "Serve the frontend instead of redirecting")
	cmd.MarkFlagsMutuallyExclusive("redirect-to", "no-redirect")
	return cmd
}

func newDomainsRemove(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "remove <frontend> <domain>",
		Short: "Remove a custom domain from a frontend",
		Long: `Remove a custom domain from a frontend.

A domain that other domains redirect to cannot be removed until they redirect
elsewhere or are removed first.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainsRemove(cmd.Context(), domainsRemoveOptions{
				deps:     deps,
				frontend: strings.TrimSpace(args[0]),
				domain:   strings.TrimSpace(args[1]),
				yes:      yes,
				in:       cmd.InOrStdin(),
				out:      cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func runDomainsList(ctx context.Context, opts domainsListOptions) error {
	frontend, domains, err := clifrontend.NewService(opts.deps).ListDomains(ctx, opts.frontend)
	if err != nil {
		return err
	}
	output.FrontendDomains(opts.out, frontend.Name, domains)
	return nil
}

func runDomainsGet(ctx context.Context, opts domainsGetOptions) error {
	frontend, domain, err := clifrontend.NewService(opts.deps).GetDomain(ctx, opts.frontend, opts.domain)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.out, "Frontend: %s\n", frontend.Name)
	output.FrontendCustomDomain(opts.out, domain)
	return nil
}

func runDomainsAdd(ctx context.Context, opts domainsAddOptions) error {
	if opts.certPath != "" {
		if err := readCertificate(&opts); err != nil {
			return err
		}
	}
	frontend, domain, err := clifrontend.NewService(opts.deps).AddDomain(ctx, opts.frontend, opts.input)
	if err != nil {
		return err
	}
	output.Success(opts.out, "Custom domain '%s' added to frontend '%s'", domain.Domain, frontend.Name)
	output.FrontendCustomDomain(opts.out, domain)
	return nil
}

func readCertificate(opts *domainsAddOptions) error {
	var err error
	if opts.input.CertificatePEM, err = readPEMFile(opts.certPath, "certificate"); err != nil {
		return err
	}
	if opts.input.PrivateKeyPEM, err = readPEMFile(opts.keyPath, "private key"); err != nil {
		return err
	}
	if opts.chainPath != "" {
		opts.input.CertificateChainPEM, err = readPEMFile(opts.chainPath, "certificate chain")
	}
	return err
}

func runDomainsUpdate(ctx context.Context, opts domainsUpdateOptions) error {
	frontend, domain, err := clifrontend.NewService(opts.deps).SetDomainRedirect(ctx, opts.frontend, opts.domain, opts.redirect)
	if err != nil {
		return err
	}
	if domain.Redirect != nil {
		output.Success(opts.out, "Custom domain '%s' now redirects to '%s' with %d", domain.Domain, domain.Redirect.To, domain.Redirect.Status)
	} else {
		output.Success(opts.out, "Custom domain '%s' now serves frontend '%s'", domain.Domain, frontend.Name)
	}
	output.FrontendCustomDomain(opts.out, domain)
	return nil
}

func runDomainsRemove(ctx context.Context, opts domainsRemoveOptions) error {
	service := clifrontend.NewService(opts.deps)
	frontend, domain, err := service.GetDomain(ctx, opts.frontend, opts.domain)
	if err != nil {
		return err
	}

	if !opts.yes {
		confirmed, err := confirm.Delete(opts.in, opts.out, "custom domain", fmt.Sprintf("%s on frontend %s", domain.Domain, frontend.Name))
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}
	}

	if err := service.DeleteDomain(ctx, frontend.Id, domain.Domain); err != nil {
		return err
	}
	output.Success(opts.out, "Custom domain '%s' removal started", domain.Domain)
	return nil
}
