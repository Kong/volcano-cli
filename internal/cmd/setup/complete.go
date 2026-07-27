package setupcmd

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/setup"
)

// errInterrupted is returned when the user aborts an in-flight install (ctrl+c),
// so the command exits non-zero and never reports a partial install as success.
var errInterrupted = errors.New("setup interrupted")

// revealInterval paces the completion reveal: report lines appear in turn so the
// results read as "printing in" rather than snapping on all at once.
const revealInterval = 40 * time.Millisecond

// holdAfterReveal keeps the finished frame on screen briefly before exiting, so
// the completion doesn't blink away the instant the last line lands.
const holdAfterReveal = 500 * time.Millisecond

// runInteractive installs behind a spinner and animates the completion report,
// mirroring the non-interactive report's content. TTY-only: the plain
// RenderReport still serves pipes, CI, and agents.
func runInteractive(cmd *cobra.Command, opts setup.Options, color bool) error {
	// A cancellable context so aborting the picker's completion stops the in-flight
	// install (setup.Run honors ctx), rather than leaving it writing after exit.
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()

	progOpts := []tea.ProgramOption{tea.WithInput(cmd.InOrStdin()), tea.WithOutput(cmd.OutOrStdout())}
	if !color {
		progOpts = append(progOpts, tea.WithColorProfile(colorprofile.Ascii))
	}
	final, err := tea.NewProgram(newCompleteModel(ctx, cancel, opts, color), progOpts...).Run()
	if err != nil {
		return err
	}
	m, ok := final.(completeModel)
	switch {
	case !ok:
		return nil
	case m.err != nil:
		return m.err
	case m.report.Failed():
		return errHarnessFailed
	default:
		return nil
	}
}

type installDoneMsg struct {
	report setup.Report
	err    error
}

type revealMsg struct{}

// completeModel runs setup.Run behind the erupting-volcano animation, then
// reveals the report line by line. It renders inline (no alt-screen) so the
// finished report stays in the terminal scrollback after the program exits.
type completeModel struct {
	ctx        context.Context
	cancel     context.CancelFunc
	opts       setup.Options
	color      bool
	width      int // terminal width, tracked from WindowSizeMsg, for detail wrapping
	tick       int // eruption animation step
	installing bool
	report     setup.Report
	err        error
	lines      []string
	shown      int
}

func newCompleteModel(ctx context.Context, cancel context.CancelFunc, opts setup.Options, color bool) completeModel {
	return completeModel{ctx: ctx, cancel: cancel, opts: opts, color: color, installing: true}
}

func (m completeModel) Init() tea.Cmd {
	return tea.Batch(eruptionTick(), m.install)
}

// install runs the real setup off the UI goroutine and reports the outcome.
func (m completeModel) install() tea.Msg {
	report, err := setup.Run(m.ctx, m.opts)
	return installDoneMsg{report: report, err: err}
}

func (m completeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			// Interrupt: cancel the in-flight install and exit non-zero so a partial
			// install is never reported as success.
			if m.installing {
				m.cancel()
				m.err = errInterrupted
				return m, tea.Quit
			}
			m.shown = len(m.lines)
			return m, tea.Quit
		case "q", "esc", "enter":
			// Dismiss the finished report. Ignored while still installing so a
			// lingering Enter from the picker can't abort the install mid-flight.
			if !m.installing {
				m.shown = len(m.lines)
				return m, tea.Quit
			}
		}
	case installDoneMsg:
		m.installing = false
		m.report, m.err = msg.report, msg.err
		if m.err != nil {
			return m, tea.Quit
		}
		m.lines = reportLines(m.report, m.color, m.width)
		return m, revealTick()
	case revealMsg:
		if m.shown < len(m.lines) {
			m.shown++
		}
		if m.shown < len(m.lines) {
			return m, revealTick()
		}
		return m, tea.Tick(holdAfterReveal, func(time.Time) tea.Msg { return tea.Quit() })
	case eruptTickMsg:
		if m.installing {
			m.tick++
			return m, eruptionTick()
		}
	}
	return m, nil
}

func revealTick() tea.Cmd {
	return tea.Tick(revealInterval, func(time.Time) tea.Msg { return revealMsg{} })
}

func (m completeModel) View() tea.View {
	if m.installing {
		return tea.NewView(installView(m.tick))
	}
	if m.err != nil {
		return tea.NewView("")
	}
	var b strings.Builder
	b.WriteByte('\n')
	for i := 0; i < m.shown; i++ {
		b.WriteString(m.lines[i])
		b.WriteByte('\n')
	}
	return tea.NewView(b.String())
}

// reportLines is the report split into lines for the reveal animation, colored
// only when color is on (NO_COLOR unset) and wrapped to the terminal width so a
// long line can't overflow and corrupt the inline render. Falls back to 80
// columns if a WindowSizeMsg hasn't arrived yet (this path is always a TTY).
//
// The report already wraps detail lines (aligned to the detail column); this
// adds a safety net for the remaining lines — footers and CTAs can exceed the
// width — by wrapping only those that actually overrun, leaving already-fitting
// (and indented) lines untouched.
func reportLines(r setup.Report, color bool, width int) []string {
	if width <= 0 {
		width = 80
	}
	raw := strings.Split(strings.TrimRight(setup.RenderReportString(r, color, width), "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, ln := range raw {
		if ansi.StringWidth(ln) <= width {
			out = append(out, ln)
			continue
		}
		out = append(out, strings.Split(ansi.Wrap(ln, width, ""), "\n")...)
	}
	return out
}
