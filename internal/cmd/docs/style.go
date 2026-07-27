package docs

import (
	"io"

	"github.com/Kong/volcano-cli/internal/theme"
)

// styler wraps the shared CLI theme so docs output matches the rest of the CLI.
// It emits ANSI only to a real terminal with NO_COLOR unset (via theme.On);
// pipes, files, --json, and test buffers stay plain. The bold/faint/cyan method
// names are kept so callers read the same; they now map onto the Volcano palette
// (lava / gray / flame) instead of raw cyan.
type styler struct {
	on bool
}

func newStyler(w io.Writer) styler {
	return styler{on: theme.On(w)}
}

func (s styler) bold(text string) string  { return theme.Title(text, s.on) }   // lava, bold
func (s styler) faint(text string) string { return theme.Dim(text, s.on) }     // gray
func (s styler) cyan(text string) string  { return theme.Command(text, s.on) } // flame
