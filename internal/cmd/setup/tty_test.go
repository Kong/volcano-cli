package setupcmd

import (
	"bytes"
	"os"
	"testing"
)

// TestIsTerminalRejectsCharDevices guards the agent-safety fix: character
// devices like /dev/null satisfy os.ModeCharDevice but are not TTYs, so
// `volcano setup < /dev/null` must not be treated as interactive (which would
// launch a picker that blocks on input the caller never sends).
func TestIsTerminalRejectsCharDevices(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer func() { _ = f.Close() }()
	if isTerminal(f) {
		t.Fatalf("%s must not count as a terminal", os.DevNull)
	}
	if isTerminal(&bytes.Buffer{}) {
		t.Fatal("a non-file writer must not count as a terminal")
	}
}
