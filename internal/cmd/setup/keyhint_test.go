package setupcmd

import (
	"strings"
	"testing"
)

// TestKeyHintDescriptionColors locks the per-key hint styling so a wrong
// constant or a dropped key style can't slip through unnoticed. It asserts the
// truecolor ANSI for each accent (mirroring internal/setup theme_test.go):
// space in volcano-400, enter in volcano-600, and esc bold with no explicit
// foreground (default terminal fg). The uncolored branch must stay ANSI-free so
// NO_COLOR/piped output is unaffected.
func TestKeyHintDescriptionColors(t *testing.T) {
	if got := keyHintDescription(false); got != "space toggles, enter confirms, esc cancels" {
		t.Fatalf("keyHintDescription(false) = %q, want plain passthrough", got)
	}

	got := keyHintDescription(true)
	// volcano-400 #fb923c => 251;146;60 ; volcano-600 #ea580c => 234;88;12.
	if !strings.Contains(got, "251;146;60") {
		t.Errorf("space missing volcano-400 (251;146;60): %q", got)
	}
	if !strings.Contains(got, "234;88;12") {
		t.Errorf("enter missing volcano-600 (234;88;12): %q", got)
	}
	// esc carries no foreground color: the two orange truecolor codes are the
	// only 38;2;* sequences in the line.
	if n := strings.Count(got, "38;2;"); n != 2 {
		t.Errorf("want exactly 2 foreground colors (space, enter); esc must inherit default fg, got %d in %q", n, got)
	}
}
