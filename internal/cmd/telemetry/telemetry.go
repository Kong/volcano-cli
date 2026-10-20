// Package telemetry implements `volcano telemetry`: status, enable, disable,
// and the hidden flush command the CLI runs in the background to send an event.
package telemetry

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/telemetry"
)

// New returns the telemetry command. The setting is stored in the telemetry
// block of ~/.volcano/config.json. No `volcano telemetry` invocation is
// recorded, so turning telemetry off is not itself reported.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Show or change whether the CLI sends usage data",
		Long: `The CLI sends one usage event to the Volcano API per command: the command,
the names of the flags you set, the selected project ID (volcano cloud
commands only), the resource names
and IDs the command was given (such as function, database, or bucket names),
CLI version, OS, architecture, CPU count, CI vendor, whether stdin is a
terminal, the coding agent that ran it, whether the Volcano plugin is installed
for that agent, the exit code, and the duration. When a command fails, the
event also carries the failure type, the API error status and code, and the
error message with tokens and keys removed. Every event carries a random ID
for this machine, which volcano logout keeps. When you are logged in, events
are also linked to your account, so events sent after logout can be linked to
the account last used on this machine.

Turning telemetry off also turns off ` + "`volcano feedback`" + ` reports, which carry
text a coding agent wrote about a problem it hit, with this machine's ID.

Nothing is sent on the first run, which shows a notice instead. Shell
completion (volcano completion and pressing Tab), --offline runs, and volcano
telemetry commands are never recorded.

Turn it off with ` + "`volcano telemetry disable`" + ` or VOLCANO_TELEMETRY_DISABLED=1. A
change applies from the next command. Set VOLCANO_TELEMETRY_DEBUG=1 to print
each event instead of sending it. Full details: ` + "`volcano docs get cli/telemetry.md`" + `.`,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{telemetry.SkipAnnotation: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printStatus(cmd)
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show whether the CLI sends usage data",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return printStatus(cmd)
			},
		},
		&cobra.Command{
			Use:   "enable",
			Short: "Send usage data",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return setDisabled(cmd, false)
			},
		},
		&cobra.Command{
			Use:   "disable",
			Short: "Stop sending usage data",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return setDisabled(cmd, true)
			},
		},
		newFlush(),
	)
	return cmd
}

func printStatus(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()
	if telemetry.DisabledByEnv(os.Getenv) {
		fmt.Fprintf(w, "Telemetry is disabled by %s.\n", telemetry.EnvDisabled)
		return nil
	}
	// The setting lives only in ~/.volcano/config.json, so a broken project
	// link in the working directory does not stop status from reading it.
	cfg, err := config.LoadWithoutLink()
	if err != nil {
		return err
	}
	if cfg.Telemetry != nil && cfg.Telemetry.Disabled {
		fmt.Fprintln(w, "Telemetry is disabled. Run `volcano telemetry enable` to turn it on.")
		return nil
	}
	fmt.Fprintln(w, "Telemetry is enabled. Run `volcano telemetry disable` to turn it off.")
	return nil
}

func setDisabled(cmd *cobra.Command, disabled bool) error {
	err := config.Update(func(cfg *config.Config) (bool, error) {
		if cfg.Telemetry == nil {
			cfg.Telemetry = &config.TelemetryConfig{}
		}
		cfg.Telemetry.Disabled = disabled
		// Choosing a setting means the user has seen what telemetry is.
		cfg.Telemetry.NoticeShown = true
		return true, nil
	})
	if err != nil {
		return err
	}
	if disabled {
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry is disabled.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry is enabled.")
	}
	if !disabled && telemetry.DisabledByEnv(os.Getenv) {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: %s is set, so no data is sent until it is unset.\n", telemetry.EnvDisabled)
	}
	return nil
}

// newFlush is the hidden command the CLI starts in the background to send one
// event read from stdin.
func newFlush() *cobra.Command {
	return &cobra.Command{
		Use:    "flush",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return telemetry.Flush(cmd.Context(), cmd.InOrStdin(), nil)
		},
	}
}
