package setupcmd

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/Kong/volcano-cli/internal/setup"
)

// eruptInterval paces the eruption loop (~9 frames/sec).
const eruptInterval = 110 * time.Millisecond

const (
	emberHex = "#fb923c" // volcano-400: sparks / embers
	terraHex = "#d4613e" // lava-600: the mountain
)

type eruptTickMsg struct{}

func eruptionTick() tea.Cmd {
	return tea.Tick(eruptInterval, func(time.Time) tea.Msg { return eruptTickMsg{} })
}

// eruptionFrames is the looping volcano-eruption shown while setup installs:
// dormant -> stir -> build -> launch -> peak -> spread -> fall -> settle, then
// repeat. Every frame is 7 fixed-width rows so the mountain stays put while the
// plume animates above the crater.
var eruptionFrames = [][]string{
	{"           ", "           ", "           ", "     .     ", "   /###\\   ", "  /     \\  ", " /_______\\ "},
	{"           ", "           ", "     .     ", "    . .    ", "   /###\\   ", "  /     \\  ", " /_______\\ "},
	{"           ", "     .     ", "    ...    ", "    ***    ", "   /###\\   ", "  /     \\  ", " /_______\\ "},
	{"     .     ", "    ...    ", "   *****   ", "   *****   ", "   /###\\   ", "  /     \\  ", " /_______\\ "},
	{"  *  .  *  ", "   *   *   ", "  *******  ", "  *******  ", "   /###\\   ", "  /     \\  ", " /_______\\ "},
	{" *   .   * ", "  *  *  *  ", "   *****   ", "   ** **   ", "   /###\\   ", "  /     \\  ", " /_______\\ "},
	{" .       . ", "   .   .   ", "    . .    ", "    ***    ", "   /###\\   ", "  /     \\  ", " /_______\\ "},
	{"           ", "     .     ", "    . .    ", "     .     ", "   /###\\   ", "  /     \\  ", " /_______\\ "},
}

var (
	lavaHot   = lipgloss.NewStyle().Foreground(lipgloss.Color(setup.FlameHex))
	lavaWarm  = lipgloss.NewStyle().Foreground(lipgloss.Color(setup.VolcanoHex))
	emberSt   = lipgloss.NewStyle().Foreground(lipgloss.Color(emberHex))
	terraSt   = lipgloss.NewStyle().Foreground(lipgloss.Color(terraHex))
	captionSt = lipgloss.NewStyle().Foreground(lipgloss.Color(setup.LavaHex)).Bold(true)
)

// renderEruption colors one frame per character. tick drives a flame/volcano
// shimmer on the lava so the eruption flickers as it loops.
func renderEruption(frame []string, tick int) string {
	var b strings.Builder
	for r, line := range frame {
		b.WriteString("  ") // left margin
		for c, ch := range line {
			switch ch {
			case '*', '#':
				st := lavaHot
				if (tick+r+c)%2 == 1 {
					st = lavaWarm
				}
				b.WriteString(st.Render(string(ch)))
			case '.':
				b.WriteString(emberSt.Render("."))
			case '/', '\\', '_':
				b.WriteString(terraSt.Render(string(ch)))
			default:
				b.WriteRune(ch)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// installView is the whole install-phase frame: the erupting volcano over a
// caption, shown while setup.Run works.
func installView(tick int) string {
	frame := eruptionFrames[tick%len(eruptionFrames)]
	return "\n" + renderEruption(frame, tick) + "\n   " + captionSt.Render("Installing Volcano…") + "\n"
}
