package setup

import (
	"io"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Kong/volcano-cli/internal/theme"
)

// The Volcano CLI palette lives in internal/theme (the CLI-wide source of
// truth). These aliases keep the picker/report and eruption animation on the
// same names they already use.
const (
	FlameHex    = theme.FlameHex
	LavaHex     = theme.LavaHex
	VolcanoHex  = theme.VolcanoHex
	OutdatedHex = theme.OutdatedHex
	GrayHex     = theme.GrayHex
)

const (
	detectedHex = OutdatedHex     // amber: detected but the install didn't complete
	failedHex   = theme.FailedHex // lava red: a hard failure
)

var (
	installedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(VolcanoHex)).Bold(true)
	detectedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(detectedHex))
	failedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(failedHex)).Bold(true)
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(failedHex)) // deep red, not bold: message text
	grayStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color(GrayHex))
)

// colorEnabled and terminalWidth delegate to internal/theme so the color gate
// and width probe live in one place; writeReport still takes them explicitly.
func colorEnabled(w io.Writer) bool { return theme.On(w) }
func terminalWidth(w io.Writer) int { return theme.Width(w) }

// styleMark colors a status mark when color is on, else returns it unchanged.
// The caller pads the mark to its column width first so the ANSI codes never
// count toward the width and throw off alignment.
func styleMark(s Status, padded string, on bool) string {
	if !on {
		return padded
	}
	switch s {
	case StatusInstalled, StatusUpdated, StatusPlanned:
		return installedStyle.Render(padded)
	case StatusUpToDate:
		// Success, but a no-op this run; gray so it recedes next to real changes.
		return grayStyle.Render(padded)
	case StatusDetected:
		return detectedStyle.Render(padded)
	case StatusFailed:
		return failedStyle.Render(padded)
	default: // StatusSkipped and any future status
		return grayStyle.Render(padded)
	}
}

// ctaBox renders the post-setup call to action inside a rounded, lava-colored
// border so it stands out from the report rows. It sizes to its content but caps
// at the terminal width, so the box never overflows (which would break the
// border and the interactive width clamp). With color off (pipes, CI, NO_COLOR)
// it falls back to the plain two-line form so machine-read output stays
// border-free.
func ctaBox(heading, example string, on bool, width int) string {
	if !on {
		return heading + "\n  " + example
	}
	boxWidth := max(ansi.StringWidth(heading), ansi.StringWidth(example)) + 4 // 2 border + 2 padding
	if width > 0 && width < boxWidth {
		boxWidth = width
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(LavaHex)).
		Foreground(lipgloss.Color(LavaHex)).
		Bold(true).
		Padding(0, 1).
		Width(boxWidth).
		Render(heading + "\n" + example)
}

// errText renders a failure/error message in deep red when on, else unchanged.
func errText(s string, on bool) string {
	if !on {
		return s
	}
	return errorStyle.Render(s)
}

// gray renders s in neutral gray when on, else returns it unchanged. Used for
// version/skill detail, supplementary metadata that should recede next to the
// colored status mark and the harness name.
func gray(s string, on bool) string {
	if !on {
		return s
	}
	return grayStyle.Render(s)
}
