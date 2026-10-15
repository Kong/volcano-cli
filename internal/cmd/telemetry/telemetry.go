// Package telemetry implements `volcano telemetry`: status, enable, disable,
// and the hidden flush command the CLI runs in the background to send an event.
package telemetry

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/telemetry"
)

// New returns the telemetry command. A nil store uses ~/.volcano/telemetry.json.
func New(store telemetry.StateStore) *cobra.Command {
	if store == nil {
		store = telemetry.FileStateStore{}
	}
	cmd := &cobra.Command{
		Use:   "telemetry",
		Short: "Show or change whether the CLI sends usage data",
		Long: `The CLI sends one usage event to the Volcano API per command: the command
and the names of the flags you set (never their values or arguments), CLI
version, OS, architecture, CPU count, CI vendor, whether stdin is a terminal,
the coding agent that ran it, whether the Volcano plugin is installed for that
agent, the exit code, and the duration. When a command run by a coding agent
fails, the event also carries the failure type and the API error status and
code. Events are linked to your account when you are logged in, and otherwise
to a random ID for this machine.

Nothing is sent on the first run, which shows a notice instead. Shell
completion and --offline runs are never recorded.

Turn it off with ` + "`volcano telemetry disable`" + `, VOLCANO_TELEMETRY_DISABLED=1, or
DO_NOT_TRACK=1. Set VOLCANO_TELEMETRY_DEBUG=1 to print each event instead of
sending it. Full details: ` + "`volcano docs get cli/telemetry.md`" + `.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printStatus(cmd, store)
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "status",
			Short: "Show whether the CLI sends usage data",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return printStatus(cmd, store)
			},
		},
		&cobra.Command{
			Use:   "enable",
			Short: "Send usage data",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return setDisabled(cmd, store, false)
			},
		},
		&cobra.Command{
			Use:   "disable",
			Short: "Stop sending usage data",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, _ []string) error {
				return setDisabled(cmd, store, true)
			},
		},
		newFlush(),
	)
	return cmd
}

func printStatus(cmd *cobra.Command, store telemetry.StateStore) error {
	w := cmd.OutOrStdout()
	if name := telemetry.DisabledByEnv(os.Getenv); name != "" {
		fmt.Fprintf(w, "Telemetry is disabled by %s.\n", name)
		return nil
	}
	state, err := store.Load()
	if err != nil {
		return err
	}
	if state.Disabled {
		fmt.Fprintln(w, "Telemetry is disabled. Run `volcano telemetry enable` to turn it on.")
		return nil
	}
	fmt.Fprintln(w, "Telemetry is enabled. Run `volcano telemetry disable` to turn it off.")
	return nil
}

func setDisabled(cmd *cobra.Command, store telemetry.StateStore, disabled bool) error {
	err := store.Update(func(state *telemetry.State) bool {
		state.Disabled = disabled
		// Choosing a setting means the user has seen what telemetry is.
		state.NoticeShown = true
		return true
	})
	if err != nil {
		return err
	}
	if disabled {
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry is disabled.")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Telemetry is enabled.")
	}
	if name := telemetry.DisabledByEnv(os.Getenv); name != "" && !disabled {
		fmt.Fprintf(cmd.ErrOrStderr(), "Note: %s is set, so no data is sent until it is unset.\n", name)
	}
	return nil
}

// newFlush is the hidden command the CLI starts in the background to send one
// event read from stdin. Its own invocation is never recorded.
func newFlush() *cobra.Command {
	return &cobra.Command{
		Use:         "flush",
		Hidden:      true,
		Args:        cobra.NoArgs,
		Annotations: map[string]string{telemetry.SkipAnnotation: "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			return telemetry.Flush(cmd.Context(), cmd.InOrStdin(), nil)
		},
	}
}
