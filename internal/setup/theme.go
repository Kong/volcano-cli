package setup

import (
	"io"
	"os"

	"charm.land/lipgloss/v2"
)

// BrandHex is the Volcano brand accent (lava flame), taken from volcano-web's UI
// theme. The interactive picker and the report both key off it so the CLI keeps
// one color identity instead of dropping to plain text between the two.
const BrandHex = "#f54019"

const (
	detectedHex = "#eab308" // amber: detected but the install didn't complete
	failedHex   = "#dc2626" // lava red: a hard failure
)

var (
	brandStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(BrandHex)).Bold(true)
	detectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(detectedHex))
	failedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(failedHex)).Bold(true)
	faintStyle    = lipgloss.NewStyle().Faint(true)
)

// colorEnabled reports whether ANSI styling should be written to w: only when w
// is a real terminal and NO_COLOR is unset. Piped output, files, and test
// buffers (agents/CI) stay plain, so a machine-read report is never polluted
// with escape codes.
func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// styleMark colors a status mark when color is on, else returns it unchanged.
// The caller pads the mark to its column width first so the ANSI codes never
// count toward the width and throw off alignment.
func styleMark(s Status, padded string, on bool) string {
	if !on {
		return padded
	}
	switch s {
	case StatusInstalled, StatusPlanned:
		return brandStyle.Render(padded)
	case StatusDetected:
		return detectedStyle.Render(padded)
	case StatusFailed:
		return failedStyle.Render(padded)
	default: // StatusSkipped and any future status
		return faintStyle.Render(padded)
	}
}

// brand renders s in the brand accent when on, else returns it unchanged.
func brand(s string, on bool) string {
	if !on {
		return s
	}
	return brandStyle.Render(s)
}
