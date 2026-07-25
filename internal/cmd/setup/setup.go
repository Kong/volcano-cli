// Package setupcmd wires the volcano setup command.
package setupcmd

import (
	"errors"

	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/setup"
)

// New returns the setup command.
func New(deps cliruntime.Deps) *cobra.Command {
	var harnesses []string
	var manual, dryRun bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Install Volcano agent skills/plugins into your coding agents",
		Long: `Detect the coding-agent harnesses installed on this machine and install the
Volcano plugin/skills into each, then report what was set up.

With no flags, setup autodetects and installs for every detected harness
(claude-code, codex, cursor, opencode, pi). Harnesses with a non-interactive
plugin command (claude-code, codex) are installed via their marketplace; the
rest have the Volcano skills written into their skills directory. If no harness
is detected, skills are installed under ~/.volcano as a manual fallback.

  volcano setup                       Autodetect and install for all detected harnesses
  volcano setup --harness claude-code Install only for the named harness(es)
  volcano setup --manual              Force the ~/.volcano manual install
  volcano setup --dry-run             Show what would be installed, change nothing`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := setup.Run(cmd.Context(), setup.Options{
				HTTPDoer: deps.HTTPClient,
				Only:     harnesses,
				Manual:   manual,
				DryRun:   dryRun,
			})
			if err != nil {
				return err
			}
			setup.RenderReport(cmd.OutOrStdout(), report)
			if report.Failed() {
				return errors.New("one or more harnesses failed to set up")
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&harnesses, "harness", nil, "Install only for the named harness(es): claude-code, codex, cursor, opencode, pi, manual")
	cmd.Flags().BoolVar(&manual, "manual", false, "Force a manual install of skills under ~/.volcano")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be installed without making changes")
	return cmd
}
