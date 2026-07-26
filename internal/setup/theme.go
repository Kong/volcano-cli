package setup

import (
	"io"
	"os"

	"charm.land/lipgloss/v2"
)

// The Volcano CLI palette, taken from volcano-web. Shared by the interactive
// picker and the report so the CLI keeps one color identity across both.
const (
	FlameHex    = "#f54019" // lava flame: key hints (space/enter/esc)
	LavaHex     = "#f37a58" // lava-500 brand primary: titles, main lines, CTA
	VolcanoHex  = "#f97316" // volcano-500: options — selectors, checkboxes, marks
	OutdatedHex = "#eab308" // amber: installed but a newer version is available
)

const (
	detectedHex = OutdatedHex // amber: detected but the install didn't complete
	failedHex   = "#dc2626"   // lava red: a hard failure
)

var (
	installedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(VolcanoHex)).Bold(true)
	detectedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(detectedHex))
	failedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(failedHex)).Bold(true)
	errorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(failedHex)) // deep red, not bold: message text
	ctaStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color(LavaHex)).Bold(true)
	faintStyle     = lipgloss.NewStyle().Faint(true)
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
	case StatusInstalled, StatusUpdated, StatusPlanned:
		return installedStyle.Render(padded)
	case StatusUpToDate:
		// Success, but a no-op this run; dim so it recedes next to real changes.
		return faintStyle.Render(padded)
	case StatusDetected:
		return detectedStyle.Render(padded)
	case StatusFailed:
		return failedStyle.Render(padded)
	default: // StatusSkipped and any future status
		return faintStyle.Render(padded)
	}
}

// cta renders s in the lava CTA accent when on, else returns it unchanged.
func cta(s string, on bool) string {
	if !on {
		return s
	}
	return ctaStyle.Render(s)
}

// errText renders a failure/error message in deep red when on, else unchanged.
func errText(s string, on bool) string {
	if !on {
		return s
	}
	return errorStyle.Render(s)
}

// faint dims s when on, else returns it unchanged. Used for up-to-date rows,
// whose no-op detail should recede next to harnesses that actually changed.
func faint(s string, on bool) string {
	if !on {
		return s
	}
	return faintStyle.Render(s)
}
