// Package config wires the config command tree for declarative project
// configuration.
package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// pulledManifestMode keeps pulled manifests owner-only: variable values are
// included in exports.
const pulledManifestMode = 0o600

type deployOptions struct {
	deps   cliruntime.Deps
	file   string
	dryRun bool
	out    io.Writer
}

type pullOptions struct {
	deps  cliruntime.Deps
	file  string
	force bool
	out   io.Writer
}

// New returns the config command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage declarative project configuration",
		Long: `Manage the full project configuration through a declarative YAML manifest
(volcano-config.yaml): project settings, database assertions, variables,
buckets and policies, realtime, the complete auth configuration (providers,
email, templates, managed pages), function visibility and schedulers, and
frontend custom domains.

The server owns validation and reconciliation; the CLI uploads and downloads
the manifest.`,
	}
	cmd.AddCommand(newDeploy(deps))
	cmd.AddCommand(newPull(deps))
	return cmd
}

func newDeploy(deps cliruntime.Deps) *cobra.Command {
	var file string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy project configuration from YAML",
		Long: `Deploy project configuration from a declarative YAML manifest.

The manifest is uploaded in a single request; the server validates everything
first (nothing is applied on validation failure) and then reconciles each
declared section. Declared config sections are the source of truth: variables,
bucket policies, OAuth providers, email templates, and function schedulers are
fully synced, so entries absent from the manifest are deleted. Omitted
sections and fields are left untouched. Functions, frontends, databases, and
buckets are never created or deleted through the manifest.

${ENV_VAR} references are interpolated from the CLI environment before upload;
a reference to an unset variable is an error. Use $$ for a literal $.

Use --dry-run to print the projected actions (including skipped/missing
warnings and operational notices) without changing anything — for example to
validate a manifest in CI.

If --file is omitted, the CLI looks for (in order):
  1. volcano/volcano-config.yaml (recommended)
  2. ./volcano-config.yaml (project root)`,
		Example: fmt.Sprintf(`  %s
  %s
  %s`,
			cliruntime.CommandPath(deps, "config deploy"),
			cliruntime.CommandPath(deps, "config deploy -f volcano-config.yaml"),
			cliruntime.CommandPath(deps, "config deploy --dry-run")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeploy(cmd.Context(), deployOptions{
				deps:   deps,
				file:   file,
				dryRun: dryRun,
				out:    cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to configuration YAML file (default: volcano/volcano-config.yaml or ./volcano-config.yaml)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and print projected actions without applying changes")
	return cmd
}

func newPull(deps cliruntime.Deps) *cobra.Command {
	var file string
	var force bool
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Download the current project configuration as YAML",
		Long: `Download the project's current configuration as a canonical
volcano-config.yaml rendered by the server.

Write-only secrets (SMTP password, OAuth client secrets, custom domain TLS
material) are omitted from the export; set them via ${ENV_VAR} interpolation
before deploying. Variable values are included.

Without --file the manifest is written to an existing manifest location, or
volcano/volcano-config.yaml when the volcano directory exists, else
./volcano-config.yaml. An existing file is not overwritten unless --force is
given.`,
		Example: fmt.Sprintf(`  %s
  %s`,
			cliruntime.CommandPath(deps, "config pull"),
			cliruntime.CommandPath(deps, "config pull -f volcano-config.yaml --force")),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPull(cmd.Context(), pullOptions{
				deps:  deps,
				file:  file,
				force: force,
				out:   cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to write the configuration YAML file to (default: volcano/volcano-config.yaml or ./volcano-config.yaml)")
	cmd.Flags().BoolVar(&force, "force", false, "Overwrite the target file if it already exists")
	return cmd
}

func runDeploy(ctx context.Context, opts deployOptions) error {
	manifestPath, err := projectconfig.ResolveManifestPath(opts.file)
	if err != nil {
		return err
	}

	manifest, resolvedPath, err := projectconfig.Load(manifestPath)
	if err != nil {
		return err
	}

	result, err := projectconfig.NewService(opts.deps).Deploy(ctx, manifest, opts.dryRun)
	if err != nil {
		var validationErr *api.ProjectConfigValidationError
		if errors.As(err, &validationErr) {
			output.ProjectConfigValidationErrors(opts.out, validationErr.Errors)
			return validationErr
		}
		if isConfigEndpointMissing(err) {
			return errors.New("this server does not support declarative config apply; upgrade your local-mode server image and try again")
		}
		return fmt.Errorf("failed to deploy configuration from %s: %w", manifestPath, err)
	}

	if opts.dryRun {
		fmt.Fprintln(opts.out, "Dry run: projected actions, nothing was applied.")
	} else {
		output.Success(opts.out, "Configuration deployed from %s", filepath.Base(resolvedPath))
	}
	output.ProjectConfigApplyReport(opts.out, result)

	if result != nil && result.Summary.Errors > 0 {
		return fmt.Errorf("%d configuration change(s) failed to apply; see the report above", result.Summary.Errors)
	}
	return nil
}

func runPull(ctx context.Context, opts pullOptions) error {
	targetPath := strings.TrimSpace(opts.file)
	if targetPath == "" {
		targetPath = projectconfig.DefaultPullPath()
	}

	if !opts.force {
		if _, err := os.Stat(targetPath); err == nil {
			return fmt.Errorf("refusing to overwrite existing file %s (use --force to replace it)", targetPath)
		}
	}

	manifest, err := projectconfig.NewService(opts.deps).Pull(ctx)
	if err != nil {
		if isConfigEndpointMissing(err) {
			return errors.New("this server does not support declarative config export; upgrade your local-mode server image and try again")
		}
		return fmt.Errorf("failed to download configuration: %w", err)
	}

	if dir := filepath.Dir(targetPath); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}
	if err := writePulledManifest(targetPath, manifest); err != nil {
		return err
	}

	output.Success(opts.out, "Configuration written to %s", targetPath)
	output.Note(opts.out, "write-only secrets (SMTP password, OAuth client secrets, TLS material) are omitted; set them via ${ENV_VAR} interpolation before deploying")
	return nil
}

// writePulledManifest writes the pulled manifest owner-only (pulledManifestMode)
// even when overwriting an existing file. os.WriteFile only applies its mode
// when it creates the file, so a --force overwrite of a pre-existing 0644
// manifest would keep the looser mode and leave the exported variable values
// readable by other local users. Writing a fresh 0600 temp file in the target
// directory and renaming it into place makes the write atomic and guarantees
// the owner-only mode regardless of any pre-existing file.
func writePulledManifest(targetPath string, manifest []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(targetPath), ".volcano-config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temporary configuration file: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup of the temp file if we fail before the rename lands.
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmp.Chmod(pulledManifestMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to secure temporary configuration file %s: %w", tmpPath, err)
	}
	if _, err := tmp.Write(manifest); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write configuration to %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to finalize configuration file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("failed to write configuration to %s: %w", targetPath, err)
	}
	return nil
}

// isConfigEndpointMissing detects a 404 from a server without the config
// routes (an older local-mode image): the router's plain "404 page not found"
// body rather than a JSON project-not-found error.
func isConfigEndpointMissing(err error) bool {
	return api.Status(err) == 404 && strings.Contains(err.Error(), "page not found")
}
