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
	if got := cta("hi", false); got != "hi" {
		t.Fatalf("cta off = %q, want plain passthrough", got)
	}
	if got := cta("hi", true); !strings.Contains(got, "\x1b[") {
		t.Fatalf("cta on = %q, want ANSI-wrapped", got)
	}
	if got := styleMark(StatusFailed, "[fail]", true); !strings.Contains(got, "\x1b[") {
		t.Fatalf("styleMark on = %q, want ANSI-wrapped", got)
	}
}

// TestRenderReportString checks the colored/plain split the completion animation
// relies on: plain is ANSI-free (what pipes get), colored carries ANSI, and both
// contain the same underlying text.
func TestRenderReportString(t *testing.T) {
	r := Report{Results: []Result{{Harness: "cursor", Status: StatusInstalled, Detail: "12 skills"}}}
	plain := RenderReportString(r, false)
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("plain report contains ANSI: %q", plain)
	}
	colored := RenderReportString(r, true)
	if !strings.Contains(colored, "\x1b[") {
		t.Fatal("colored report missing ANSI")
	}
	if !strings.Contains(plain, "cursor") || !strings.Contains(plain, "Installed Volcano") {
		t.Fatalf("plain report missing expected content: %q", plain)
	}
}
