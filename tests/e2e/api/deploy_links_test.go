package api

import (
	"encoding/json"
	"testing"
)

// TestAPIE2ECloudDeployLinks walks the deploy-link group as far as a project
// without a GitHub connection allows. Creating a link needs a project connected
// to a public repository its owner can push to, which the harness cannot set
// up, so this covers what a fresh project can show: no links, an unknown id
// refused on every route that takes one, and the refusal to create a link for a
// project with no repository.
func TestAPIE2ECloudDeployLinks(t *testing.T) {
	env := setupAPIE2E(t, "cloud-deploy-links")
	env.loginAndUse(t)

	env.runCloudCLI(t, "deploy-link", "list").requireSuccess(t, "No deploy links found")
	listed := env.runCloudCLI(t, "deploy-link", "list", "--json")
	listed.requireSuccess(t)
	var links []json.RawMessage
	if err := json.Unmarshal([]byte(listed.stdout), &links); err != nil {
		t.Fatalf("deploy-link list --json printed something other than an array: %v\n%s", err, listed.output)
	}
	if len(links) != 0 {
		t.Fatalf("a new project has %d deploy link(s), want none:\n%s", len(links), listed.stdout)
	}

	const unknownID = "cli-e2e-no-such-link"
	for _, args := range [][]string{
		{"deploy-link", "get", unknownID},
		{"deploy-link", "stats", unknownID},
		{"deploy-link", "disable", unknownID},
		{"deploy-link", "enable", unknownID},
		{"deploy-link", "delete", unknownID, "--yes"},
	} {
		env.runCloudCLI(t, args...).requireFailure(t, "HTTP 404")
	}

	env.runCloudCLI(t, "deploy-link", "create").
		requireFailure(t, "HTTP 409", "git connect")
}
