package docs

import (
	"io"
	"os"
)

// styler emits ANSI styling only when writing to a real terminal and NO_COLOR
// is unset. Piped output, files, --json, and test buffers stay plain. It uses
// raw escape codes to avoid adding a styling dependency.
type styler struct {
	enabled bool
}

func newStyler(w io.Writer) styler {
	return styler{enabled: colorEnabled(w)}
}

func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiFaint = "\x1b[2m"
	ansiCyan  = "\x1b[36m"
)

func (s styler) wrap(code, text string) string {
	if !s.enabled || text == "" {
		return text
	}
	return code + text + ansiReset
}

func (s styler) bold(text string) string  { return s.wrap(ansiBold, text) }
func (s styler) faint(text string) string { return s.wrap(ansiFaint, text) }
func (s styler) cyan(text string) string  { return s.wrap(ansiCyan, text) }
