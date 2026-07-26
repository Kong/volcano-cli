package setup

import (
	"bytes"
	"strings"
	"testing"
)

// TestColorGate locks in the agent-safety contract: non-terminal writers (pipes,
// CI, agents, test buffers) never get ANSI, and the style helpers are plain
// pass-throughs when color is off.
func TestColorGate(t *testing.T) {
	if colorEnabled(&bytes.Buffer{}) {
		t.Fatal("colorEnabled true for a non-file writer; pipes/agents would get ANSI")
	}
	if got := styleMark(StatusInstalled, "[ok]", false); got != "[ok]" {
		t.Fatalf("styleMark off = %q, want plain passthrough", got)
	}
	if got := brand("hi", false); got != "hi" {
		t.Fatalf("brand off = %q, want plain passthrough", got)
	}
	if got := brand("hi", true); !strings.Contains(got, "\x1b[") {
		t.Fatalf("brand on = %q, want ANSI-wrapped", got)
	}
	if got := styleMark(StatusFailed, "[fail]", true); !strings.Contains(got, "\x1b[") {
		t.Fatalf("styleMark on = %q, want ANSI-wrapped", got)
	}
}
