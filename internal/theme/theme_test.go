package theme

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// TestOnGate locks the agent-safety contract: non-terminal writers (pipes, CI,
// agents, test buffers) never get ANSI, and NO_COLOR forces plain even on a
// terminal. The terminal check is stubbed so the NO_COLOR branch is actually
// exercised (a bytes.Buffer is false before NO_COLOR is ever consulted).
func TestOnGate(t *testing.T) {
	if On(&bytes.Buffer{}) {
		t.Fatal("On true for a non-file writer; pipes/agents would get ANSI")
	}

	orig := isTerminal
	isTerminal = func(*os.File) bool { return true }
	defer func() { isTerminal = orig }()

	t.Setenv("NO_COLOR", "")
	if !On(os.Stdout) {
		t.Fatal("On should be true for a terminal with NO_COLOR unset")
	}
	t.Setenv("NO_COLOR", "1")
	if On(os.Stdout) {
		t.Fatal("On must be false when NO_COLOR is set, even on a terminal")
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

// TestStatusInProgressStatesAreAmber ensures transitional API states
// (deleting/detaching/pending_verification) map to the in-progress amber style
// rather than falling through to gray. Guards against new transitional enum
// values landing in the default bucket.
func TestStatusInProgressStatesAreAmber(t *testing.T) {
	amberPrefix, _, _ := strings.Cut(Status("provisioning", true), "provisioning")
	if amberPrefix == "" {
		t.Fatal("expected an ANSI prefix for an amber state")
	}
	for _, s := range []string{"deleting", "detaching", "pending_verification"} {
		if !strings.HasPrefix(Status(s, true), amberPrefix) {
			t.Fatalf("%q should use the amber in-progress style", s)
		}
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
