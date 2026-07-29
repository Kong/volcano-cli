// Package setupcmd wires the volcano setup command.
package setupcmd

import (
	"errors"
	"fmt"
	"os"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
	"github.com/Kong/volcano-cli/internal/setup"
)

// errHarnessFailed is returned when any targeted harness failed to install, so
// the command exits non-zero in both the plain and animated paths.
var errHarnessFailed = errors.New("one or more harnesses failed to set up")

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
  volcano setup --agent claude-code   Install only for the named agent(s)
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
				// On a TTY, honor NO_COLOR for the whole interactive path (picker theme
				// and animated report); lipgloss/huh don't strip color on their own.
				color := os.Getenv("NO_COLOR") == ""
				selected, cancelled, err := promptHarnesses(cmd, opts, color)
				if err != nil {
					return err
				}
				if cancelled {
					fmt.Fprintln(cmd.OutOrStdout(), "Setup cancelled.")
					return nil
				}
				// selected is nil when nothing was detected; leave Only empty so
				// Run autodetects and does its ~/.volcano manual fallback. For a picked
				// set, keep autodetect's best-effort failure policy so accepting the
				// default doesn't fail/exit-nonzero over a set that --yes would tolerate.
				if len(selected) > 0 {
					opts.Only = selected
					opts.BestEffort = true
				}
				// Install behind a spinner, then animate the completion report.
				return runInteractive(cmd, opts, color)
			}
			report, err := setup.Run(cmd.Context(), opts)
			if err != nil {
				return err
			}
			setup.RenderReport(cmd.OutOrStdout(), report)
			if report.Failed() {
				return errHarnessFailed
			}
			return nil
		},
	}

	cmd.Flags().StringSliceVar(&harnesses, "agent", nil, "Install only for the named agent(s): claude-code, codex, cursor, opencode, pi, manual")
	cmd.Flags().BoolVar(&manual, "manual", false, "Force a manual install of skills under ~/.volcano")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be installed without making changes")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the prompt and install for all detected harnesses (agent/CI-safe)")
	// --manual and --agent contradict (Run gives --agent precedence), so
	// reject the combination up front rather than silently ignoring --manual.
	cmd.MarkFlagsMutuallyExclusive("agent", "manual")
	// --yes means "all detected", which contradicts targeting a specific set.
	cmd.MarkFlagsMutuallyExclusive("agent", "yes")
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
func promptHarnesses(cmd *cobra.Command, opts setup.Options, color bool) (selected []string, cancelled bool, err error) {
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
	outdatedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(setup.OutdatedHex))
	grayStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(setup.GrayHex))
	options := make([]huh.Option[string], len(detected))
	for i, d := range detected {
		mark := d.StatusMark()
		note := d.VersionNote()
		if color {
			// An outdated harness gets the amber accent so "can be updated" reads at a
			// glance; installed/available keep the brand orange. The version hint is
			// gray so it recedes behind the harness name.
			if d.Updatable() {
				mark = outdatedStyle.Render(mark)
			} else {
				mark = markStyle.Render(mark)
			}
			if note != "" {
				note = grayStyle.Render(note)
			}
		}
		options[i] = huh.NewOption(mark+" "+d.Name+note, d.Name).Selected(true)
	}

	// huh's default quit binding is ctrl+c only; bind esc too so the advertised
	// "esc cancels" hint actually aborts the picker.
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "cancel"))

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Install Volcano for which coding agents?").
				Description(keyHintDescription(color)).
				Options(options...).
				Value(&selected),
		),
	).WithInput(cmd.InOrStdin()).WithOutput(cmd.OutOrStdout()).WithKeyMap(km)
	if color {
		form = form.WithTheme(volcanoTheme())
	} else {
		// Strip all color (including huh's default theme) under NO_COLOR.
		form = form.WithProgramOptions(tea.WithColorProfile(colorprofile.Ascii))
	}

	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return nil, true, nil
		}
		return nil, false, err
	}
	return selected, len(selected) == 0, nil
}

// keyHintDescription renders the picker's key hint with the actual keystrokes
// (space/enter/esc) each in a distinct accent so they read as keys, not prose,
// and stand apart from each other. Each segment is styled in full — colored
// keys, dimmed connectors — so huh's own description style can't leave the line
// half-rendered after an embedded reset.
func keyHintDescription(color bool) string {
	if !color {
		return "space toggles, enter confirms, esc cancels"
	}
	// Distinct accents so the three keys read apart at a glance: space in
	// volcano-400, enter in volcano-600, and esc in the terminal's default
	// foreground (typically white on dark, black on light) so it pops against the
	// two oranges without pinning a hex the user's theme may have remapped.
	dim := lipgloss.NewStyle().Faint(true)
	keyStyle := func(hex string) lipgloss.Style {
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(hex))
	}
	spaceStyle := keyStyle(setup.Volcano400Hex)
	enterStyle := keyStyle(setup.Volcano600Hex)
	escStyle := lipgloss.NewStyle().Bold(true) // no explicit fg: inherits the terminal's default foreground
	hint := func(st lipgloss.Style, k, rest string) string { return st.Render(k) + dim.Render(rest) }
	return hint(spaceStyle, "space", " toggles, ") + hint(enterStyle, "enter", " confirms, ") + hint(escStyle, "esc", " cancels")
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

// isTerminal reports whether v is a real TTY. It uses term.IsTerminal (an actual
// terminal query) rather than an os.ModeCharDevice check, because character
// devices like /dev/null also satisfy ModeCharDevice: `volcano setup < /dev/null`
// from a shell would otherwise pass the check and launch a picker that blocks on
// input the caller never sends. Buffers, pipes, and non-TTY char devices are not
// terminals, so they never prompt.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(f.Fd())
}
