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
	if got := styleMark(StatusInstalled, "[installed]", false); got != "[installed]" {
		t.Fatalf("styleMark off = %q, want plain passthrough", got)
	}
	if got := ctaBox("hi", "there", false, 0); got != "hi\n  there" {
		t.Fatalf("ctaBox off = %q, want plain two-line passthrough", got)
	}
	got := ctaBox("hi", "there", true, 40)
	if !strings.Contains(got, "\x1b[") || !strings.Contains(got, "\u256d") {
		t.Fatalf("ctaBox on = %q, want ANSI + rounded border", got)
	}
	// The example line renders in volcano-600 (#ea580c => 234;88;12 truecolor),
	// distinct from the lava heading/border, so it reads as the actionable prompt.
	if !strings.Contains(got, "234;88;12") {
		t.Fatalf("ctaBox on = %q, want example in volcano-600", got)
	}
	if got := styleMark(StatusFailed, "[failed]", true); !strings.Contains(got, "\x1b[") {
		t.Fatalf("styleMark on = %q, want ANSI-wrapped", got)
	}
}

// TestRenderReportString checks the colored/plain split the completion animation
// relies on: plain is ANSI-free (what pipes get), colored carries ANSI, and both
// contain the same underlying text.
func TestRenderReportString(t *testing.T) {
	r := Report{Results: []Result{{Harness: "cursor", Status: StatusInstalled, Detail: "12 skills"}}}
	plain := RenderReportString(r, false, 0)
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain report contains ANSI: %q", plain)
	}
	colored := RenderReportString(r, true, 0)
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("colored report missing ANSI")
	}
	if !strings.Contains(plain, "cursor") || !strings.Contains(plain, "Installed Volcano") {
		t.Fatalf("plain report missing expected content: %q", plain)
	}
}
