package frontends

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/apiclient"
	"github.com/Kong/volcano-cli/internal/confirm"
	clifrontend "github.com/Kong/volcano-cli/internal/frontend"
	"github.com/Kong/volcano-cli/internal/output"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type domainCreateOptions struct {
	deps       cliruntime.Deps
	identifier string
	domain     string
	tlsMode    string
	certPath   string
	keyPath    string
	chainPath  string
	out        io.Writer
}

type domainGetOptions struct {
	deps       cliruntime.Deps
	identifier string
	out        io.Writer
}

type domainDeleteOptions struct {
	deps       cliruntime.Deps
	identifier string
	yes        bool
	in         io.Reader
	out        io.Writer
}

type domainListOptions struct {
	deps cliruntime.Deps
	out  io.Writer
}

func newDomain(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "domain",
		Short: "Manage frontend custom domains",
		Long:  "Manage custom domains attached to frontends in the current project.",
	}
	cmd.AddCommand(newDomainCreate(deps))
	cmd.AddCommand(newDomainGet(deps))
	cmd.AddCommand(newDomainDelete(deps))
	cmd.AddCommand(newDomainList(deps))
	return cmd
}

func newDomainCreate(deps cliruntime.Deps) *cobra.Command {
	var domain string
	var tlsMode string
	var certPath string
	var keyPath string
	var chainPath string
	cmd := &cobra.Command{
		Use:   "create <name-or-id>",
		Short: "Attach a custom domain to a frontend, or switch its TLS mode",
		Long: `Attach a custom domain to a frontend.

With --tls managed, Volcano issues and renews the certificate once you publish
the DNS records the command prints. With --tls byoc (the default), pass your
own certificate and unencrypted private key.

Run it again for the frontend's active domain with the other --tls mode to
switch that domain in place, without downtime. The current certificate keeps
serving until its replacement does.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "frontends domain create web --domain app.example.com --tls managed"),
			cliruntime.CommandPath(deps, "frontends domain create web --domain app.example.com --cert ./cert.pem --key ./key.pem")),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainCreate(cmd.Context(), domainCreateOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				domain:     domain,
				tlsMode:    tlsMode,
				certPath:   certPath,
				keyPath:    keyPath,
				chainPath:  chainPath,
				out:        cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVar(&domain, "domain", "", "Custom domain hostname")
	cmd.Flags().StringVar(&tlsMode, "tls", string(apiclient.FrontendCustomDomainTLSConfigModeByoc), "TLS mode: managed (Volcano issues the certificate) or byoc (bring your own)")
	cmd.Flags().StringVar(&certPath, "cert", "", "Path to PEM-encoded certificate file (byoc)")
	cmd.Flags().StringVar(&keyPath, "key", "", "Path to PEM-encoded private key file (byoc)")
	cmd.Flags().StringVar(&chainPath, "chain", "", "Path to PEM-encoded certificate chain file (byoc, optional)")
	if err := cmd.MarkFlagRequired("domain"); err != nil {
		panic(err)
	}
	return cmd
}

func newDomainGet(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "get <name-or-id>",
		Short: "Get custom domain status for a frontend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainGet(cmd.Context(), domainGetOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				out:        cmd.OutOrStdout(),
			})
		},
	}
}

func newDomainDelete(deps cliruntime.Deps) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete <name-or-id>",
		Short: "Detach the custom domain from a frontend",
		Long: `Detach the configured custom domain from a frontend.

By default this command prompts for confirmation.
Use --yes to skip the prompt.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDomainDelete(cmd.Context(), domainDeleteOptions{
				deps:       deps,
				identifier: strings.TrimSpace(args[0]),
				yes:        yes,
				in:         cmd.InOrStdin(),
				out:        cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

func newDomainList(deps cliruntime.Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List frontends with configured custom domains",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDomainList(cmd.Context(), domainListOptions{
				deps: deps,
				out:  cmd.OutOrStdout(),
			})
		},
	}
}

func runDomainCreate(ctx context.Context, opts domainCreateOptions) error {
	input, err := domainCreateInput(opts)
	if err != nil {
		return err
	}

	frontend, domain, change, err := clifrontend.NewService(opts.deps).CreateCustomDomain(ctx, opts.identifier, input)
	if err != nil {
		return err
	}

	managed := domain.TlsMode == apiclient.FrontendCustomDomainResponseTlsModeManaged
	switch {
	case change == clifrontend.CustomDomainCreated:
		output.Success(opts.out, "Custom domain '%s' created for frontend '%s'", domain.Domain, frontend.Name)
		if managed {
			output.Note(opts.out, "Publish the verification records below and keep them. Volcano issues and renews the certificate. Check progress with: %s",
				cliruntime.CommandPath(opts.deps, "frontends domain get "+frontend.Name))
		}
	case change == clifrontend.CustomDomainSwitched && managed:
		output.Success(opts.out, "Custom domain '%s' on frontend '%s' is switching to managed TLS", domain.Domain, frontend.Name)
		output.Note(opts.out, "Your uploaded certificate keeps serving until Volcano issues the managed one. Publish the verification records below and keep them.")
	case change == clifrontend.CustomDomainSwitched:
		output.Success(opts.out, "Custom domain '%s' on frontend '%s' switched to BYOC TLS", domain.Domain, frontend.Name)
		output.Note(opts.out, "Your certificate now serves, and Volcano no longer renews the managed one. You can delete the _acme-challenge CNAME.")
	default:
		output.Success(opts.out, "Custom domain '%s' already uses %s TLS on frontend '%s'", domain.Domain, domain.TlsMode, frontend.Name)
		if !managed {
			output.Warning(opts.out, "The certificate was not replaced. To rotate it, update custom_domain in volcano-config.yaml and run: %s",
				cliruntime.CommandPath(opts.deps, "config deploy"))
		}
	}
	output.FrontendCustomDomain(opts.out, domain)
	return nil
}

// domainCreateInput checks the TLS flags before anything reads a file or calls
// the API, then loads the BYOC certificate material.
func domainCreateInput(opts domainCreateOptions) (api.FrontendCustomDomainInput, error) {
	input := api.FrontendCustomDomainInput{Domain: opts.domain}
	switch mode := apiclient.FrontendCustomDomainTLSConfigMode(strings.ToLower(strings.TrimSpace(opts.tlsMode))); mode {
	case apiclient.FrontendCustomDomainTLSConfigModeManaged:
		if strings.TrimSpace(opts.certPath+opts.keyPath+opts.chainPath) != "" {
			return input, errors.New("--cert, --key, and --chain apply only to --tls byoc; Volcano issues the certificate with --tls managed")
		}
		input.TLSMode = mode
		return input, nil
	case apiclient.FrontendCustomDomainTLSConfigModeByoc:
		input.TLSMode = mode
	default:
		return input, fmt.Errorf("invalid --tls %q: use managed or byoc", opts.tlsMode)
	}

	if strings.TrimSpace(opts.certPath) == "" || strings.TrimSpace(opts.keyPath) == "" {
		return input, errors.New("--cert and --key are required with --tls byoc; use --tls managed to have Volcano issue the certificate")
	}
	var err error
	if input.CertificatePEM, err = readPEMFile(opts.certPath, "certificate"); err != nil {
		return input, err
	}
	if input.PrivateKeyPEM, err = readPEMFile(opts.keyPath, "private key"); err != nil {
		return input, err
	}
	if strings.TrimSpace(opts.chainPath) != "" {
		if input.CertificateChainPEM, err = readPEMFile(opts.chainPath, "certificate chain"); err != nil {
			return input, err
		}
	}
	return input, nil
}

func runDomainGet(ctx context.Context, opts domainGetOptions) error {
	frontend, domain, err := clifrontend.NewService(opts.deps).GetCustomDomain(ctx, opts.identifier)
	if err != nil {
		return err
	}

	fmt.Fprintf(opts.out, "Frontend: %s\n", frontend.Name)
	output.FrontendCustomDomain(opts.out, domain)
	return nil
}

func runDomainDelete(ctx context.Context, opts domainDeleteOptions) error {
	service := clifrontend.NewService(opts.deps)
	frontend, domain, err := service.GetCustomDomain(ctx, opts.identifier)
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

	if err := service.DeleteCustomDomainByID(ctx, frontend.Id); err != nil {
		return err
	}
	output.Success(opts.out, "Custom domain '%s' deletion started", domain.Domain)
	return nil
}

func runDomainList(ctx context.Context, opts domainListOptions) error {
	domains, err := clifrontend.NewService(opts.deps).ListCustomDomains(ctx)
	if err != nil {
		return err
	}

	entries := make([]output.FrontendCustomDomainEntry, 0, len(domains))
	for _, entry := range domains {
		entries = append(entries, output.FrontendCustomDomainEntry{
			FrontendName: entry.Frontend.Name,
			FrontendID:   entry.Frontend.Id.String(),
			Domain:       entry.Domain,
		})
	}
	output.FrontendCustomDomains(opts.out, entries)
	return nil
}

func readPEMFile(path, description string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s file path is required", description)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read %s file %q: %w", description, path, err)
	}
	text := strings.TrimSpace(string(payload))
	if text == "" {
		return "", fmt.Errorf("%s file %q is empty", description, path)
	}
	return text, nil
}
