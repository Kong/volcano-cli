// Package upgrade implements the top-level CLI self-upgrade command.
package upgrade

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/api"
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

// PrintAPIInstructionNotices prints a non-blocking upgrade suggestion or a
// deprecation warning based on the most recent VOL-180 instructions observed
// on this invocation's API responses (api.LastInstructions). This replaces
// the previous GitHub-release polling notice (VOL-168): the API is now the
// sole source of truth for whether/how loudly to nudge an upgrade, and the
// check adds no extra network round-trip — it only reads headers the command
// already received.
//
// A command that made no API call (help, version, completion, local-only
// commands) observes a zero-value Instructions and prints nothing.
func PrintAPIInstructionNotices(cmd *cobra.Command, deps cliruntime.Deps) {
	instructions := api.LastInstructions()
	switch instructions.CLIInstruction {
	case api.CLIInstructionVersionDeprecation:
		printDeprecationWarning(cmd, deps, instructions.LatestVersion)
	case api.CLIInstructionSuggestionUpgrade:
		printUpgradeSuggestion(cmd, deps, instructions.LatestVersion)
	}
}

func printUpgradeSuggestion(cmd *cobra.Command, deps cliruntime.Deps, latest string) {
	upgradeCmd := cliruntime.CommandPath(deps, "upgrade")
	if latest != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "A newer Volcano CLI version is available: %s (current %s). Run `%s` to upgrade.\n", latest, version.Version, upgradeCmd)
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "A newer Volcano CLI version is available (current %s). Run `%s` to upgrade.\n", version.Version, upgradeCmd)
}

// printDeprecationWarning prints a hard warning for a version the API has
// deprecated. Gated routes already 426 this request (see api.Status(err) ==
// http.StatusUpgradeRequired handling in cmd/volcano/main.go); this notice
// also fires on exempt routes (e.g. a deprecated CLI's successful `volcano
// login`), so the user learns their CLI is deprecated even when the specific
// command they ran was allowed through.
func printDeprecationWarning(cmd *cobra.Command, deps cliruntime.Deps, latest string) {
	upgradeCmd := cliruntime.CommandPath(deps, "upgrade")
	if latest != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "Volcano CLI %s is no longer supported. Upgrade to %s or later:\n  %s\n", version.Version, latest, upgradeCmd)
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Volcano CLI %s is no longer supported. Run `%s` to upgrade.\n", version.Version, upgradeCmd)
}
