package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// sharingOffMessage is what the CLI prints where the server's sharing gate is
// closed, as it is on staging until the feature launches.
const sharingOffMessage = "Sharing projects is not available in this environment."

// sharesAvailable reads the server's shares capability, the way hosting's own
// sharing E2E does, so these tests assert the branch the server is on instead
// of failing where the gate is closed.
func sharesAvailable(t *testing.T, env *apiE2E) bool {
	t.Helper()
	listed := apiE2EJSONRequest(t, http.MethodGet, env.apiURL+"/capabilities", "", nil, http.StatusOK)
	capabilities, ok := listed["data"].([]any)
	if !ok {
		t.Fatalf("capabilities carries no data array: %v", listed)
	}
	for _, entry := range capabilities {
		capability, ok := entry.(map[string]any)
		if ok && capability["id"] == "shares" {
			return capability["status"] == "available"
		}
	}
	t.Fatalf("capabilities does not list shares: %v", listed)
	return false
}

// TestAPIE2EShare walks volcano share on a fresh project, run outside a project
// directory so nothing is deployed. A fresh project has nothing deployed, so
// sharing it is refused for having nothing to share, but the share now exists: status reports it, and off switches it off. Where the gate is
// closed, sharing is refused as unavailable.
func TestAPIE2EShare(t *testing.T) {
	env := setupAPIE2E(t, "share")
	env.loginAndUse(t)

	env.runCLI(t, "share", "status").requireFailure(t, "this project is not shared yet")
	if !sharesAvailable(t, env) {
		env.runCLI(t, "share", "--yes", "--no-deploy").requireFailure(t, sharingOffMessage)
		return
	}

	env.runCLI(t, "share", "--yes", "--no-deploy", "--description", "CLI E2E share").requireFailure(t, "HTTP 422")

	status := env.runCLI(t, "share", "status", "--json")
	status.requireSuccess(t)
	var shared struct {
		Share struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Serving     bool   `json:"serving"`
		} `json:"share"`
		Release json.RawMessage `json:"release"`
	}
	if err := json.Unmarshal([]byte(status.stdout), &shared); err != nil || shared.Share.ID == "" {
		t.Fatalf("share status --json printed no share: %v\n%s", err, status.output)
	}
	if shared.Share.Description != "CLI E2E share" {
		t.Fatalf("the share kept description %q, want the one given", shared.Share.Description)
	}
	if shared.Share.Serving {
		t.Fatalf("a share with no release serves:\n%s", status.stdout)
	}
	if string(shared.Release) != "null" {
		t.Fatalf("a refused share published a release:\n%s", status.stdout)
	}
	env.runCLI(t, "share", "releases").requireSuccess(t, "No releases published yet")
	env.runCLI(t, "share", "off").requireSuccess(t, "Sharing is off.")
	env.runCLI(t, "share", "status").requireSuccess(t, "Sharing: off")
	env.runCLI(t, "share", "status", "--link", "aaaaaaaaaaaa").requireFailure(t, "HTTP 404")
}
