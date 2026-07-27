// Package theme is the single home for the Volcano CLI color identity: the
// palette, the color-enable gate (NO_COLOR + real TTY), and the small string
// helpers commands use to style output. The interactive setup path and every
// resource command share it so the whole CLI reads as one product.
//
// Every helper takes an explicit on bool and is a plain pass-through when off,
// so callers decide once (via On) whether the destination should get ANSI.
// Machine-read output (pipes, files, --json, CI, test buffers) stays plain.
package theme

import (
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

// The Volcano palette, taken from volcano-web. Kept as exported consts so the
// setup report (lipgloss styles) and any other caller reference one source.
const (
	FlameHex    = "#f54019" // lava flame: key hints, suggested commands
	LavaHex     = "#f37a58" // lava-500 brand primary: titles, table headers, CTA
	VolcanoHex  = "#f97316" // volcano-500: success, active, installed
	OutdatedHex = "#eab308" // amber: warnings, pending/in-progress states
	GrayHex     = "#6b7280" // neutral gray: summaries, separators, dim detail
	FailedHex   = "#dc2626" // lava red: errors, failed states
)

var (
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color(LavaHex)).Bold(true)
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(VolcanoHex)).Bold(true)
	activeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color(VolcanoHex))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(OutdatedHex))
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(FailedHex)).Bold(true)
	failStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color(FailedHex))
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(GrayHex))
	cmdStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color(FlameHex))
)

// On reports whether ANSI styling should be written to w: only when w is a real
// interactive terminal and NO_COLOR is unset. Piped output, files, other
// character devices (e.g. /dev/null), and test buffers stay plain so machine-read
// output is never polluted with escape codes.
func On(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isTerminal(f)
}

// isTerminal reports whether f is an interactive terminal. ModeCharDevice is not
// enough (it's also true for /dev/null), so use the terminal probe. It is a
// package var so tests can exercise the NO_COLOR / terminal gate without a real
// TTY.
var isTerminal = func(f *os.File) bool { return term.IsTerminal(f.Fd()) }

// Width returns w's column count when it is a real terminal, else 0 (unknown,
// e.g. piped/file output).
func Width(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	width, _, err := term.GetSize(f.Fd())
	if err != nil {
		return 0
	}
	return width
}

// Title styles titles and table headers (lava, bold).
func Title(s string, on bool) string { return render(titleStyle, s, on) }

// Success styles a success mark/line (volcano, bold).
func Success(s string, on bool) string { return render(successStyle, s, on) }

// Warn styles a warning prefix (amber).
func Warn(s string, on bool) string { return render(warnStyle, s, on) }

// Error styles an error prefix (red, bold).
func Error(s string, on bool) string { return render(errStyle, s, on) }

// Fail styles failure value text (red, not bold) — the counterpart to Error's
// prefix for message bodies and failed inline values.
func Fail(s string, on bool) string { return render(failStyle, s, on) }

// Dim styles summaries, separators, and detail labels (gray).
func Dim(s string, on bool) string { return render(dimStyle, s, on) }

// Command styles a suggested command or key hint (flame).
func Command(s string, on bool) string { return render(cmdStyle, s, on) }

// Status colors a status/state word by meaning. The caller pads s to its column
// width FIRST so the ANSI codes never count toward the width and break table
// alignment; Status classifies on the trimmed word but renders the padded s.
func Status(s string, on bool) string {
	if !on {
		return s
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "active", "ready", "running", "enabled", "healthy", "deployed", "verified", "yes", "public":
		return activeStyle.Render(s)
	case "pending", "provisioning", "creating", "deploying", "building", "updating",
		"deleting", "detaching", "pending_verification":
		return warnStyle.Render(s)
	case "failed", "error", "errored", "unhealthy":
		return failStyle.Render(s)
	default: // deleted, disabled, inactive, unknown, "-", no, private, ...
		return dimStyle.Render(s)
	}
}

func render(style lipgloss.Style, s string, on bool) string {
	if !on || s == "" {
		return s
	}
	return style.Render(s)
}
