// Package upgrade implements the top-level CLI self-upgrade command.
package upgrade

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/update"
	"github.com/Kong/volcano-cli/internal/version"
)

const updateCheckTimeout = 2 * time.Second

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

// MaybePrintUpdateNotice checks whether a newer CLI release is available and prints a non-blocking notice.
func MaybePrintUpdateNotice(cmd *cobra.Command, deps cliruntime.Deps) {
	if shouldSkipUpdateCheck(cmd) {
		return
	}
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, updateCheckTimeout)
	defer cancel()
	notice, err := update.CheckLatest(ctx, version.Version, updateOptions(deps))
	if err != nil {
		if errors.Is(err, update.ErrNoUpdateAvailable) {
			return
		}
		return
	}
	if notice == nil {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "A newer Volcano CLI version is available: %s (current %s). Run `volcano upgrade` to upgrade.\n", notice.Latest, notice.Current)
}

func shouldSkipUpdateCheck(cmd *cobra.Command) bool {
	if version.Version == "dev" {
		return true
	}
	if cmd.Flags().Changed("help") || cmd.Flags().Changed("version") {
		return true
	}
	if cmd.Parent() == nil && len(cmd.Flags().Args()) == 0 {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "version" || c.Name() == "upgrade" || c.Name() == "help" {
			return true
		}
	}
	for _, arg := range os.Args[1:] {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "--help" || trimmed == "-h" || trimmed == "help" || trimmed == "--version" || trimmed == "-v" {
			return true
		}
	}
	return false
}
