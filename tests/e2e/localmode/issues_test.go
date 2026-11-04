package localmode

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const localModeE2EIssueFunction = "issue-thrower"

// requireLocalModeReadsIssues deploys a function that throws, invokes it, and
// reads the issue local development records from its error through the
// issues commands. Notes are input to LavaFlow, which local development
// doesn't run, so every notes command refuses and names the cloud command.
func requireLocalModeReadsIssues(t *testing.T, binary string, env []string) {
	t.Helper()
	dir := localModeE2EProjectDir(t)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", localModeE2EIssueFunction+".js"), `
exports.handler = async () => {
	throw new TypeError("the cart is missing");
};
`)
	deployed := runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "deploy", "-f", localModeE2EIssueFunction)
	requireContains(t, deployed, "Function '"+localModeE2EIssueFunction+"' deployment started")

	info := fetchVolcanoLocalModeE2EInfo(t, env)
	functionID := waitForVolcanoLocalModeE2EFunctionID(t, info, localModeE2EIssueFunction)
	issueID := waitForLocalModeE2EIssue(t, binary, env, dir, info, functionID)

	listed := runVolcanoLocalModeE2E(t, binary, env, dir, "issues", "list", "--status", "open")
	requireContains(t, listed, issueID)
	requireContains(t, listed, localModeE2EIssueFunction)

	issue := runVolcanoLocalModeE2E(t, binary, env, dir, "issues", "get", issueID)
	requireContains(t, issue, "Issue: "+issueID)
	requireContains(t, issue, "Status: open")
	requireContains(t, issue, "Resource: function "+localModeE2EIssueFunction)
	requireContains(t, issue, "Error type: TypeError")
	requireContains(t, issue, "Exception: yes")
	requireNotContains(t, issue, "notes")

	for _, args := range [][]string{
		{"issues", "notes", issueID},
		{"issues", "notes", "add", issueID, "--text", "The cart loads after checkout starts."},
		{"issues", "notes", "edit", issueID, "--text", "The cart loads after checkout starts."},
		{"issues", "notes", "delete", issueID, "--yes"},
	} {
		output, err := runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, args...)
		if err == nil {
			t.Fatalf("expected volcano %s to fail in local mode\n%s", strings.Join(args, " "), output)
		}
		requireContains(t, output, `"issues notes" is a cloud command`)
		requireContains(t, output, "notes are input to LavaFlow")
		requireContains(t, output, "volcano cloud issues notes")
	}

	deleted := runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "delete", localModeE2EIssueFunction, "--yes")
	requireContains(t, deleted, "deletion started")
}

// waitForLocalModeE2EIssue invokes the function until its error shows up as
// an issue, and returns the issue's ID. Local development records an issue as
// it writes the error's log line.
func waitForLocalModeE2EIssue(t *testing.T, binary string, env []string, dir string, info localModeE2EInfo, functionID string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var last string
	for time.Now().Before(deadline) {
		if body, status := invokeVolcanoLocalModeE2EFunction(t, info, functionID); status == http.StatusOK {
			t.Fatalf("%s answered 200 instead of failing:\n%s", localModeE2EIssueFunction, body)
		}
		var err error
		last, err = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "issues", "list", "--json")
		if err == nil {
			if id := localModeE2EIssueFor(last, functionID); id != "" {
				return id
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("no issue appeared for %s:\n%s", localModeE2EIssueFunction, last)
	return ""
}

// localModeE2EIssueFor finds the issue the function raised in issues list
// --json output.
func localModeE2EIssueFor(output, functionID string) string {
	var page struct {
		Data []struct {
			ID       string `json:"id"`
			Resource struct {
				ID string `json:"id"`
			} `json:"resource"`
		} `json:"data"`
	}
	if start := strings.Index(output, "{"); start < 0 || json.Unmarshal([]byte(output[start:]), &page) != nil {
		return ""
	}
	for _, issue := range page.Data {
		if issue.Resource.ID == functionID {
			return issue.ID
		}
	}
	return ""
}
