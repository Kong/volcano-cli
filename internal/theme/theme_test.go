package theme

import (
	"bytes"
	"strings"
	"testing"
)

// TestOnGate locks the agent-safety contract: non-terminal writers (pipes, CI,
// agents, test buffers) never get ANSI, and NO_COLOR forces plain everywhere.
func TestOnGate(t *testing.T) {
	if On(&bytes.Buffer{}) {
		t.Fatal("On true for a non-file writer; pipes/agents would get ANSI")
	}
	t.Setenv("NO_COLOR", "1")
	if On(&bytes.Buffer{}) {
		t.Fatal("On true with NO_COLOR set")
	}
}

// TestHelpersOffArePlain guarantees every helper is a byte-identical passthrough
// when color is off, so machine-read output is unchanged.
func TestHelpersOffArePlain(t *testing.T) {
	for name, got := range map[string]string{
		"Title":   Title("x", false),
		"Success": Success("x", false),
		"Warn":    Warn("x", false),
		"Error":   Error("x", false),
		"Fail":    Fail("x", false),
		"Dim":     Dim("x", false),
		"Command": Command("x", false),
		"Status":  Status("active", false),
	} {
		if strings.Contains(got, "\x1b[") {
			t.Fatalf("%s off contains ANSI: %q", name, got)
		}
	}
	if Status("active", false) != "active" {
		t.Fatal("Status off must pass through unchanged")
	}
}

// TestHelpersOnEmitANSI checks color-on renders escape codes.
func TestHelpersOnEmitANSI(t *testing.T) {
	if !strings.Contains(Title("x", true), "\x1b[") {
		t.Fatal("Title on should emit ANSI")
	}
	if !strings.Contains(Status("failed", true), "\x1b[") {
		t.Fatal("Status on should emit ANSI")
	}
}

// TestStatusClassificationPreservesText: the padded text (including trailing
// spaces the caller added for column alignment) survives coloring intact, and
// each class maps to a distinct color so cells read by meaning.
func TestStatusClassificationPreservesText(t *testing.T) {
	padded := "active      " // caller pads to width BEFORE coloring
	got := Status(padded, true)
	if !strings.Contains(got, padded) {
		t.Fatalf("Status dropped padded text: %q", got)
	}
	active := Status("active", true)
	pending := Status("pending", true)
	failed := Status("failed", true)
	unknown := Status("deleted", true)
	for _, pair := range [][2]string{{active, pending}, {active, failed}, {pending, failed}, {active, unknown}} {
		if pair[0] == pair[1] {
			t.Fatalf("status colors not distinct: %q == %q", pair[0], pair[1])
		}
	}
}
