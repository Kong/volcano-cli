package localmode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// requireLocalModeSendsFeedback checks `volcano feedback` from a local-mode
// project. Reports always go to the cloud API, so this lane, which has no
// cloud, saves them with VOLCANO_FEEDBACK_DEBUG instead of sending them, and
// gives the CLI a login token the server never sees. The suite turns telemetry
// off, which also turns feedback off until `volcano feedback enable`, so the
// report is anonymous.
func requireLocalModeSendsFeedback(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	env = append(append([]string{}, env...), "VOLCANO_TOKEN=local-feedback-e2e", "VOLCANO_FEEDBACK_DEBUG=1")
	reports := filepath.Join(localModeE2EHome(t, env), ".volcano", "feedback.jsonl")
	report := []string{
		"feedback",
		"--category", "cli_bug",
		"--summary", "local-mode feedback smoke",
		"--details", "Sent by the local-mode E2E suite.",
		"--command", "volcano status",
	}

	status := runVolcanoLocalModeE2E(t, binary, env, dir, "feedback", "status")
	requireContains(t, status, "Feedback is disabled because telemetry is off.")
	if output := runVolcanoLocalModeE2E(t, binary, env, dir, report...); strings.TrimSpace(output) != "" {
		t.Fatalf("expected no output while feedback is off\n%s", output)
	}
	if _, err := os.Stat(reports); !os.IsNotExist(err) {
		t.Fatalf("expected no report while feedback is off, stat %s: %v", reports, err)
	}

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "feedback", "enable"), "Feedback is enabled.")
	t.Cleanup(func() { _, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "feedback", "disable") })
	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, "feedback", "status"), "Feedback is enabled.")

	dryRun := runVolcanoLocalModeE2E(t, binary, env, dir, append(report, "--dry-run")...)
	requireContains(t, dryRun, `"summary": "local-mode feedback smoke"`)
	requireNotContains(t, dryRun, "would not be sent")

	requireContains(t, runVolcanoLocalModeE2E(t, binary, env, dir, report...), "Feedback sent. ID: ")
	data, err := os.ReadFile(reports)
	if err != nil {
		t.Fatalf("failed to read the saved report: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one saved report, got %d\n%s", len(lines), data)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &fields); err != nil {
		t.Fatalf("failed to parse the saved report: %v\n%s", err, lines[0])
	}
	if fields["anonymous"] != true {
		t.Fatalf("expected an anonymous report while telemetry is off\n%s", lines[0])
	}
	for _, key := range []string{"device_id", "project_id"} {
		if _, ok := fields[key]; ok {
			t.Fatalf("expected no %s in an anonymous report\n%s", key, lines[0])
		}
	}
	if fields["category"] != "cli_bug" || fields["summary"] != "local-mode feedback smoke" {
		t.Fatalf("unexpected report\n%s", lines[0])
	}
}

// localModeE2EHome is the HOME the suite gives the CLI: the last one in env.
func localModeE2EHome(t *testing.T, env []string) string {
	t.Helper()
	home := ""
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "HOME="); ok {
			home = value
		}
	}
	if home == "" {
		t.Fatal("the local-mode environment has no HOME")
	}
	return home
}
