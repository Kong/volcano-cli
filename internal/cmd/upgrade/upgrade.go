// Package upgrade implements the top-level CLI self-upgrade command.
package upgrade

import (
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/update"
	"github.com/Kong/volcano-cli/internal/version"
)

// New returns the upgrade command.
func New(deps cliruntime.Deps) *cobra.Command {
	var verifySignature bool
	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade Volcano CLI to the latest release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := updateOptions(deps)
			opts.RequireSignatureVerification = verifySignature
			return update.Upgrade(cmd.Context(), version.Version, cmd.OutOrStdout(), opts)
		},
	}
	cmd.Flags().BoolVar(&verifySignature, "verify-signature", false, "Require Sigstore signature verification with cosign")
	return cmd
}

func updateOptions(deps cliruntime.Deps) update.Options {
	return update.Options{
		HTTPClient:     deps.HTTPClient,
		GitHubAPIURL:   deps.UpdateGitHubAPIURL,
		ExecutablePath: deps.ExecutablePath,
		CommandRunner:  deps.UpdateCommandRunner,
	}
}
