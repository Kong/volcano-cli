// Package config wires the `volcano config` command tree, which deploys
// declarative cloud project configuration from a volcano-config.yaml manifest.
package config

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/output"
	"github.com/Kong/volcano-cli/internal/projectconfig"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

type deployOptions struct {
	deps cliruntime.Deps
	file string
	out  io.Writer
}

// New returns the config command.
func New(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage declarative cloud project configuration",
		Long: `Deploy declarative cloud project configuration from YAML manifests.

This command group is designed for expanding configuration management over time.
Currently it supports:
  - Storage buckets
  - Storage policies
  - Function visibility (public/private)`,
	}
	cmd.AddCommand(newDeploy(deps))
	return cmd
}

func newDeploy(deps cliruntime.Deps) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy cloud project configuration from YAML",
		Long: `Deploy cloud project configuration from a declarative YAML file.

Currently supported resources:
  - Storage buckets
  - Storage policies
  - Function visibility (public/private)

Reconciliation semantics:
  - Bucket attributes (file_size_limit, allowed_mime_types) merge: omitted
    fields leave the existing server value untouched.
  - Storage policies are reconciled exhaustively: any policy on the server
    that is not present in the manifest's bucket "policies:" list is DELETED.
    Omitting "policies:" entirely (or setting it to []) deletes every policy
    on that bucket.

If --file is omitted, the CLI looks for (in order):
  1. volcano/volcano-config.yaml (recommended)
  2. ./volcano-config.yaml (project root)`,
		Example: `  volcano config deploy
  volcano config deploy -f volcano-config.yaml`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDeploy(cmd.Context(), deployOptions{
				deps: deps,
				file: file,
				out:  cmd.OutOrStdout(),
			})
		},
	}
	cmd.Flags().StringVarP(&file, "file", "f", "", "Path to configuration YAML file (default: volcano/volcano-config.yaml or ./volcano-config.yaml)")
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

	summary, err := projectconfig.NewService(opts.deps).Deploy(ctx, manifest)
	if err != nil {
		if summary != nil {
			output.ProjectConfigDeploySummary(opts.out, summary)
		}
		return fmt.Errorf("failed to deploy configuration from %s: %w", manifestPath, err)
	}

	output.Success(opts.out, "Configuration deployed from %s", filepath.Base(resolvedPath))
	output.ProjectConfigDeploySummary(opts.out, summary)
	return nil
}
