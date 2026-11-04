package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// issuesNotAvailable is what every issues read says where issues aren't
// available in the environment.
const issuesNotAvailable = "issues aren't available in this Volcano environment yet"

// apiE2EUnknownID names no issue, note or run in any project.
const apiE2EUnknownID = "00000000-0000-4000-8000-000000000000"

// apiE2ENoteText is an ordinary note, which no text check refuses.
const apiE2ENoteText = "Coupons without an expiry date never expire."

// TestAPIE2ECloudIssues reads a fresh project's issues and checks who may
// write notes, the way a user does. The lane's servers record no issues, so
// the project has none: the reads come back empty or not found, and a note
// can't be written at all. Kong/volcano-hosting's Live Infra Smoke covers
// issues raised from real errors.
//
// Writing a note is a person's act. The lane's owner token never expires,
// which Volcano refuses for notes in its own words, and a project access
// token reads issues but the CLI refuses it for note writes before it sends
// anything. Both exit 5.
func TestAPIE2ECloudIssues(t *testing.T) {
	env := setupAPIE2E(t, "cloud-issues")
	env.loginAndUse(t)

	probe := env.runCloudCLI(t, "issues", "list")
	if strings.Contains(probe.output, issuesNotAvailable) {
		probe.requireFailure(t)
		env.runCloudCLI(t, "issues", "get", apiE2EUnknownID).requireFailure(t, issuesNotAvailable)
		requireAPIE2EProjectTokenWritesNoNotes(t, env, false)
		return
	}

	probe.requireSuccess(t, "No issues")
	listed := env.runCloudCLI(t, "issues", "list", "--status", "open", "--json")
	listed.requireSuccess(t)
	var page struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(listed.stdout), &page); err != nil {
		t.Fatalf("issues list --json did not emit JSON: %v\n%s", err, listed.output)
	}
	if len(page.Data) != 0 || page.HasMore {
		t.Fatalf("a fresh project listed issues:\n%s", listed.output)
	}

	requireAPIE2EExit(t, env.runCloudCLI(t, "issues", "get", apiE2EUnknownID), 4, "failed to get issue "+apiE2EUnknownID)
	requireAPIE2EExit(t, env.runCloudCLI(t, "issues", "notes", apiE2EUnknownID), 4,
		"failed to list the notes on issue "+apiE2EUnknownID)
	// Volcano checks who writes before it looks for the issue.
	requireAPIE2EExit(t, env.runCloudCLI(t, "issues", "notes", "add", apiE2EUnknownID, "--text", apiE2ENoteText), 5,
		"the note wasn't saved: ", "never expires")
	requireAPIE2EProjectTokenWritesNoNotes(t, env, true)
}

// requireAPIE2EProjectTokenWritesNoNotes checks that a project access token
// can't write, edit or delete notes, and, where issues are available, still
// reads them.
func requireAPIE2EProjectTokenWritesNoNotes(t *testing.T, env *apiE2E, available bool) {
	t.Helper()
	name := "cli-e2e-issues-" + apiE2ESuffix(t)
	created := env.runCloudCLI(t, "access-tokens", "create", name, "--scope", "full", "--json")
	_, secret := apiE2ECreatedAccessToken(t, created)
	t.Cleanup(func() {
		if revoked := env.runCloudCLI(t, "access-tokens", "revoke", name, "--yes"); revoked.code != 0 {
			t.Errorf("failed to revoke the access token this test minted:\n%s", redactCredentials(revoked.output))
		}
	})

	projectTokenEnv := []string{"HOME=" + t.TempDir(), "VOLCANO_TOKEN=" + secret}
	env.runCLIWithEnv(t, projectTokenEnv, "use", env.projectID).requireSuccess(t, "Now using project")
	if available {
		env.runCloudCLIWithEnv(t, projectTokenEnv, "issues", "list").requireSuccess(t, "No issues")
	}
	refusal := "writing issue notes needs a person's session: a project access token can't"
	for _, args := range [][]string{
		{"issues", "notes", "add", apiE2EUnknownID, "--text", apiE2ENoteText},
		{"issues", "notes", "edit", apiE2EUnknownID, "--text", apiE2ENoteText},
		{"issues", "notes", "delete", apiE2EUnknownID, "--yes"},
	} {
		requireAPIE2EExit(t, env.runCloudCLIWithEnv(t, projectTokenEnv, args...), 5, refusal)
	}
}

// requireAPIE2EExit checks that a command failed with code, saying each of
// needles.
func requireAPIE2EExit(t *testing.T, result cliResult, code int, needles ...string) {
	t.Helper()
	result.requireFailure(t, needles...)
	if result.code != code {
		t.Fatalf("command exited %d, want %d:\n%s", result.code, code, redactCredentials(result.output))
	}
}
