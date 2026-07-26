package setupcmd

import (
	"context"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"github.com/Kong/volcano-cli/internal/setup"
)

// revealInterval paces the completion reveal: report lines appear in turn so the
// results read as "printing in" rather than snapping on all at once.
const revealInterval = 40 * time.Millisecond

// holdAfterReveal keeps the finished frame on screen briefly before exiting, so
// the completion doesn't blink away the instant the last line lands.
const holdAfterReveal = 500 * time.Millisecond

// runInteractive installs behind a spinner and animates the completion report,
// mirroring the non-interactive report's content. TTY-only: the plain
// RenderReport still serves pipes, CI, and agents.
func runInteractive(cmd *cobra.Command, opts setup.Options) error {
	final, err := tea.NewProgram(
		newCompleteModel(cmd.Context(), opts),
		tea.WithInput(cmd.InOrStdin()),
		tea.WithOutput(cmd.OutOrStdout()),
	).Run()
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

// completeModel runs setup.Run behind a spinner, then reveals the report line by
// line. It renders inline (no alt-screen) so the finished report stays in the
// terminal scrollback after the program exits.
type completeModel struct {
	ctx        context.Context
	opts       setup.Options
	spin       spinner.Model
	installing bool
	report     setup.Report
	err        error
	lines      []string
	shown      int
}

func newCompleteModel(ctx context.Context, opts setup.Options) completeModel {
	sp := spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(setup.VolcanoHex))),
	)
	return completeModel{ctx: ctx, opts: opts, spin: sp, installing: true}
}

func (m completeModel) Init() tea.Cmd {
	return tea.Batch(m.spin.Tick, m.install)
}

// install runs the real setup off the UI goroutine and reports the outcome.
func (m completeModel) install() tea.Msg {
	report, err := setup.Run(m.ctx, m.opts)
	return installDoneMsg{report: report, err: err}
}

func (m completeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc", "enter":
			m.shown = len(m.lines) // reveal everything, then leave
			return m, tea.Quit
		}
	case installDoneMsg:
		m.installing = false
		m.report, m.err = msg.report, msg.err
		if m.err != nil {
			return m, tea.Quit
		}
		m.lines = reportLines(m.report)
		return m, revealTick()
	case revealMsg:
		if m.shown < len(m.lines) {
			m.shown++
		}
		if m.shown < len(m.lines) {
			return m, revealTick()
		}
		return m, tea.Tick(holdAfterReveal, func(time.Time) tea.Msg { return tea.Quit() })
	case spinner.TickMsg:
		if m.installing {
			var cmd tea.Cmd
			m.spin, cmd = m.spin.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

func revealTick() tea.Cmd {
	return tea.Tick(revealInterval, func(time.Time) tea.Msg { return revealMsg{} })
}

func (m completeModel) View() tea.View {
	if m.installing {
		title := lipgloss.NewStyle().Foreground(lipgloss.Color(setup.LavaHex)).Bold(true).Render("Installing Volcano…")
		return tea.NewView("\n  " + m.spin.View() + " " + title + "\n")
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

// reportLines is the colored report split into lines for the reveal animation.
func reportLines(r setup.Report) []string {
	return strings.Split(strings.TrimRight(setup.RenderReportString(r, true), "\n"), "\n")
}
