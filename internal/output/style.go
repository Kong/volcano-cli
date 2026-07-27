package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/Kong/volcano-cli/internal/theme"
)

// This file holds the color-aware print helpers shared by every renderer. They
// key color off theme.On(w): a real TTY with NO_COLOR unset gets ANSI; pipes,
// files, --json, CI, and test buffers stay plain, so machine-read output is
// unchanged. Callers compute on := theme.On(w) once and pass it down.

// tableHead prints a lava-bold header row followed by a gray separator rule of
// ruleLen dashes. lead adds a leading blank line first (some lists print one).
// format must NOT include a trailing newline.
func tableHead(w io.Writer, on, lead bool, ruleLen int, format string, cols ...any) {
	if lead {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, theme.Title(fmt.Sprintf(format, cols...), on))
	fmt.Fprintln(w, theme.Dim(strings.Repeat("-", ruleLen), on))
}

// statusCell pads a status/state word to width, then colors it by meaning. Pad
// happens first so ANSI never counts toward the column width.
func statusCell(status string, width int, on bool) string {
	return theme.Status(fmt.Sprintf("%-*s", width, status), on)
}

// kv writes a "Label: value" detail line with the label dimmed when on.
func kv(w io.Writer, on bool, label, format string, args ...any) {
	fmt.Fprintf(w, "%s %s\n", theme.Dim(label+":", on), fmt.Sprintf(format, args...))
}

// summary writes a dimmed supplementary line (page counts, totals) with a
// leading blank line, matching the prior "\n...\n" spacing.
func summary(w io.Writer, on bool, format string, args ...any) {
	fmt.Fprintf(w, "\n%s\n", theme.Dim(fmt.Sprintf(format, args...), on))
}

// nextPage writes a dimmed "Next page: " label followed by the suggested command
// in flame, with a leading blank line.
func nextPage(w io.Writer, on bool, command string) {
	fmt.Fprintf(w, "\n%s%s\n", theme.Dim("Next page: ", on), theme.Command(command, on))
}
