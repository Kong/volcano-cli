package setupcmd

import (
	"context"
	"strings"
	"testing"

	"github.com/Kong/volcano-cli/internal/setup"
)

// TestCompleteModelReveal exercises the completion state machine without running
// the bubbletea loop: install finishes, the report is split into lines, reveal
// ticks advance one line at a time, and the fully-revealed view carries the same
// content the non-interactive report prints.
func TestCompleteModelReveal(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m := newCompleteModel(ctx, cancel, setup.Options{}, true)
	if !m.installing {
		t.Fatal("model should start in the installing (eruption) phase")
	}
	if v := m.View().Content; !strings.Contains(v, "Installing Volcano") {
		t.Fatalf("installing view missing caption: %q", v)
	}

	report := setup.Report{Results: []setup.Result{
		{Harness: "cursor", Status: setup.StatusInstalled, Detail: "12 skills"},
	}}
	next, _ := m.Update(installDoneMsg{report: report})
	m = next.(completeModel)
	if m.installing {
		t.Fatal("still installing after installDoneMsg")
	}
	if len(m.lines) == 0 {
		t.Fatal("no report lines queued for reveal")
	}

	for m.shown < len(m.lines) {
		next, _ = m.Update(revealMsg{})
		m = next.(completeModel)
	}

	view := m.View().Content
	if !strings.Contains(view, "cursor") || !strings.Contains(view, "Installed Volcano") {
		t.Fatalf("fully-revealed view missing report content: %q", view)
	}
}
