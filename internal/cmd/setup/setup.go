// Package setupcmd wires the volcano setup command.
package setupcmd

import (
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/setup"
)

// New returns the setup command.
func New(deps cliruntime.Deps) *cobra.Command {
	var harnesses []string
	var manual, dryRun, yes bool

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

On a real terminal with no targeting flags, setup asks which detected harnesses
to install. It stays non-interactive (installs all detected) when stdin/stdout
is piped, when CI or VOLCANO_NONINTERACTIVE is set, or when --yes is passed, so
agents and scripts never block on a prompt.

  volcano setup                       Autodetect; prompt on a terminal, install all otherwise
  volcano setup --yes                 Install all detected, no prompt (use this in agents/CI)
  volcano setup --harness claude-code Install only for the named harness(es)
  volcano setup --manual              Force the ~/.volcano manual install
  volcano setup --dry-run             Show what would be installed, change nothing`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			opts := setup.Options{
				HTTPDoer: deps.HTTPClient,
				Only:     harnesses,
				Manual:   manual,
				DryRun:   dryRun,
			}
			if interactive(cmd, harnesses, manual, dryRun, yes) {
				selected, cancelled, err := promptHarnesses(cmd, opts)
				if err != nil {
					return err
				}
				if cancelled {
					fmt.Fprintln(cmd.OutOrStdout(), "Setup cancelled.")
					return nil
				}
				// selected is nil when nothing was detected; leave Only empty so
				// Run autodetects and does its ~/.volcano manual fallback.
				if len(selected) > 0 {
					opts.Only = selected
				}
			}
			report, err := setup.Run(cmd.Context(), opts)
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
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the prompt and install for all detected harnesses (agent/CI-safe)")
	// --manual and --harness contradict (Run gives --harness precedence), so
	// reject the combination up front rather than silently ignoring --manual.
	cmd.MarkFlagsMutuallyExclusive("harness", "manual")
	// --yes means "all detected", which contradicts targeting a specific set.
	cmd.MarkFlagsMutuallyExclusive("harness", "yes")
	cmd.MarkFlagsMutuallyExclusive("manual", "yes")
	return cmd
}

// interactive reports whether setup should prompt for harness selection.
// Non-interactive (the agent/CI-safe default) whenever a targeting or preview
// flag or --yes is set, when CI/VOLCANO_NONINTERACTIVE/TERM=dumb signals a
// non-terminal environment, or when stdin/stdout is not a real terminal.
func interactive(cmd *cobra.Command, harnesses []string, manual, dryRun, yes bool) bool {
	if len(harnesses) > 0 || manual || dryRun || yes {
		return false
	}
	if os.Getenv("CI") != "" || os.Getenv("VOLCANO_NONINTERACTIVE") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTerminal(cmd.InOrStdin()) && isTerminal(cmd.OutOrStdout())
}

// promptHarnesses detects installed harnesses and shows a checkbox TUI to pick
// which to set up. It returns the chosen names, or cancelled=true when the user
// clears the selection or aborts (esc/ctrl+c). With no harness detected it
// returns (nil, false, nil) so the caller falls through to Run's
// autodetect/manual fallback.
func promptHarnesses(cmd *cobra.Command, opts setup.Options) (selected []string, cancelled bool, err error) {
	detected, err := setup.Detect(opts)
	if err != nil {
		return nil, false, err
	}
	if len(detected) == 0 {
		return nil, false, nil
	}

	// Color only the [installed]/[available] mark (volcano); the harness name
	// stays in the terminal's default foreground so it reads white on dark and
	// black on light backgrounds. Pre-select every detected harness so a straight
	// Enter installs all, matching the non-interactive default.
	markStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(setup.VolcanoHex))
	options := make([]huh.Option[string], len(detected))
	for i, d := range detected {
		label := markStyle.Render(d.StatusMark()) + " " + d.Name
		options[i] = huh.NewOption(label, d.Name).Selected(true)
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Install Volcano for which coding agents?").
				Description(keyHintDescription()).
				Options(options...).
				Value(&selected),
		),
	).WithInput(cmd.InOrStdin()).WithOutput(cmd.OutOrStdout()).WithTheme(volcanoTheme())

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return selected, len(selected) == 0, nil
}

// keyHintDescription renders the picker's key hint with the actual keystrokes
// (space/enter/esc) in the brand accent so they read as keys, not prose. Each
// segment is styled in full — colored keys, dimmed connectors — so huh's own
// description style can't leave the line half-rendered after an embedded reset.
func keyHintDescription() string {
	key := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(setup.FlameHex))
	dim := lipgloss.NewStyle().Faint(true)
	hint := func(k, rest string) string { return key.Render(k) + dim.Render(rest) }
	return hint("space", " toggles, ") + hint("enter", " confirms, ") + hint("esc", " cancels")
}

// volcanoTheme brands the picker: the title in lava, and the option selector and
// checkboxes in volcano orange. Option text has its foreground unset so harness
// names render in the terminal's default color (adaptive to light/dark), leaving
// only the marks and checkboxes carrying brand color.
func volcanoTheme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		s := huh.ThemeBase(isDark)
		lava := lipgloss.Color(setup.LavaHex)
		volcano := lipgloss.Color(setup.VolcanoHex)
		s.Focused.Title = s.Focused.Title.Foreground(lava).Bold(true)
		s.Focused.SelectSelector = s.Focused.SelectSelector.Foreground(volcano)
		s.Focused.MultiSelectSelector = s.Focused.MultiSelectSelector.Foreground(volcano)
		s.Focused.SelectedPrefix = s.Focused.SelectedPrefix.Foreground(volcano)
		s.Focused.UnselectedPrefix = s.Focused.UnselectedPrefix.Foreground(volcano)
		s.Focused.SelectedOption = s.Focused.SelectedOption.UnsetForeground()
		s.Focused.Option = s.Focused.Option.UnsetForeground()
		return s
	})
}

// isTerminal reports whether v is a real character device (a TTY), using the
// stdlib os.ModeCharDevice check rather than adding a terminal dependency.
// Buffers and pipes (tests, agents, CI) are not terminals, so they never prompt.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, statErr := f.Stat()
	return statErr == nil && info.Mode()&os.ModeCharDevice != 0
}
