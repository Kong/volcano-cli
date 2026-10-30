package api

import (
	"encoding/json"
	"strings"
	"testing"
)

// lavaflowNotYours is what every LavaFlow command says, with a nonzero exit,
// where the project's capabilities leave LavaFlow out for the account.
const lavaflowNotYours = "LavaFlow isn't available to your account yet"

// lavaflowCommands is every LavaFlow command, each with --json.
var lavaflowCommands = [][]string{
	{"lavaflow", "status", "--json"},
	{"lavaflow", "enable", "--yes", "--json"},
	{"lavaflow", "disable", "--json"},
	{"lavaflow", "settings", "--json"},
	{"lavaflow", "settings", "set", "--autonomy", "diagnose_only", "--json"},
	{"lavaflow", "stats", "--json"},
	{"lavaflow", "resources", "--json"},
	{"lavaflow", "list", "--json"},
	{"lavaflow", "show", "00000000-0000-4000-8000-000000000000", "--json"},
}

// TestAPIE2ECloudLavaFlow runs LavaFlow's commands against a fresh project the
// way a user does. The project has no issues and no connected repository, so
// no run starts and nothing can open a pull request; Kong/volcano-hosting's
// cloud E2E covers runs.
//
// LavaFlow is open to a list of accounts. A fresh account is not on it, so
// every command says LavaFlow isn't available to it and exits nonzero. Where
// the environment opens LavaFlow to every account, the round trip runs
// instead: settings saved by one command read back by the next, and a project
// access token reads but can't change them.
func TestAPIE2ECloudLavaFlow(t *testing.T) {
	env := setupAPIE2E(t, "cloud-lavaflow")
	env.loginAndUse(t)

	if probe := env.runCloudCLI(t, "lavaflow", "status"); strings.Contains(probe.output, lavaflowNotYours) {
		for _, command := range lavaflowCommands {
			env.runCloudCLI(t, command...).requireFailure(t, lavaflowNotYours)
		}
		return
	}

	// Registered after the project's own cleanup, so it runs first: LavaFlow is
	// off before the project is deleted, even when an assertion below fails.
	t.Cleanup(func() { env.runCloudCLI(t, "lavaflow", "disable").requireSuccess(t, "LavaFlow is off") })

	env.runCloudCLI(t, "lavaflow", "status").requireSuccess(t, "LavaFlow: off", "Repository: none connected",
		"Turn it on with volcano cloud lavaflow enable")
	env.runCloudCLI(t, "lavaflow", "enable", "--yes").requireSuccess(t,
		"LavaFlow sends this resource's logs and source code to an AI service that Volcano operates.", "LavaFlow is on")
	env.runCloudCLI(t, "lavaflow", "settings", "set", "--autonomy", "diagnose_only",
		"--custom-context", "cli-e2e context").requireSuccess(t, "LavaFlow's settings are saved")

	settings := env.runCloudCLI(t, "lavaflow", "settings", "--json")
	settings.requireSuccess(t)
	var saved struct {
		Enabled       bool   `json:"enabled"`
		Autonomy      string `json:"autonomy"`
		CustomContext string `json:"custom_context"`
	}
	if err := json.Unmarshal([]byte(settings.stdout), &saved); err != nil {
		t.Fatalf("settings --json did not emit JSON: %v\n%s", err, settings.output)
	}
	if !saved.Enabled || saved.Autonomy != "diagnose_only" || saved.CustomContext != "cli-e2e context" {
		t.Fatalf("settings did not read back what was saved: %+v", saved)
	}

	env.runCloudCLI(t, "lavaflow", "stats", "--days", "7").requireSuccess(t, "0 issue(s) opened and 0 fix pull request(s) opened")
	env.runCloudCLI(t, "lavaflow", "resources").requireSuccess(t)
	env.runCloudCLI(t, "lavaflow", "list").requireSuccess(t, "No runs")
	requireAPIE2ELavaFlowNeedsAPerson(t, env)

	env.runCloudCLI(t, "lavaflow", "disable").requireSuccess(t, "LavaFlow is off")
	env.runCloudCLI(t, "lavaflow", "status").requireSuccess(t, "LavaFlow: off")
}

// requireAPIE2ELavaFlowNeedsAPerson checks a project access token reads
// LavaFlow but can't change it.
func requireAPIE2ELavaFlowNeedsAPerson(t *testing.T, env *apiE2E) {
	t.Helper()
	name := "cli-e2e-lavaflow-" + apiE2ESuffix(t)
	created := env.runCloudCLI(t, "access-tokens", "create", name, "--scope", "full", "--json")
	_, secret := apiE2ECreatedAccessToken(t, created)
	t.Cleanup(func() {
		if revoked := env.runCloudCLI(t, "access-tokens", "revoke", name, "--yes"); revoked.code != 0 {
			t.Errorf("failed to revoke the access token this test minted:\n%s", redactCredentials(revoked.output))
		}
	})

	projectTokenEnv := []string{"HOME=" + t.TempDir(), "VOLCANO_TOKEN=" + secret}
	env.runCLIWithEnv(t, projectTokenEnv, "use", env.projectID).requireSuccess(t, "Now using project")
	env.runCloudCLIWithEnv(t, projectTokenEnv, "lavaflow", "settings").requireSuccess(t, "LavaFlow: on")
	env.runCloudCLIWithEnv(t, projectTokenEnv, "lavaflow", "disable").
		requireFailure(t, "changing LavaFlow's settings needs a person's session")
}
