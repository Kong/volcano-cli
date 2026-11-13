package localmode

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// requireLocalModeSearchesProjectLogs reads the callback workflow's own output
// through the project-wide logs commands: the durable kind, a name, a query,
// and JSON for both the logs and their counts.
func requireLocalModeSearchesProjectLogs(t *testing.T, binary string, env []string, dir string) {
	t.Helper()

	output := runVolcanoLocalModeE2E(t, binary, env, dir,
		"logs", "--kind", "durable", "--name", "callback-pipeline", "--query", `"callback-token refund"`, "--json")
	var events []map[string]any
	for line := range strings.Lines(output) {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("volcano logs --json printed a line that is not JSON: %v\n%s", err, output)
		}
		events = append(events, event)
	}
	if len(events) == 0 {
		t.Fatalf("volcano logs found none of the refund callback's output:\n%s", output)
	}
	for _, event := range events {
		body, _ := event["body"].(string)
		resource, _ := event["resource"].(map[string]any)
		if !strings.Contains(body, "callback-token refund") || resource["type"] != "function" {
			t.Fatalf("volcano logs returned a log the query does not match:\n%s", output)
		}
	}

	// Counts come from their own read model, which can trail the logs.
	deadline := time.Now().Add(2 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		var err error
		last, err = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir,
			"logs", "activity", "--kind", "durable", "--query", `"callback-token"`, "--json")
		var activity struct {
			Total int `json:"total"`
		}
		if start := strings.Index(last, "{"); err == nil && start >= 0 &&
			json.Unmarshal([]byte(last[start:]), &activity) == nil && activity.Total >= 2 {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("volcano logs activity never counted both callback tokens:\n%s", last)
}
