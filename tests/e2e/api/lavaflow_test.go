package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Kong/volcano-cli/internal/apiclient"
)

// What every LavaFlow command says, with a nonzero exit, for a project
// without LavaFlow records where LavaFlow isn't open to it or its owner, and
// where the owner's plan doesn't include LavaFlow.
const (
	lavaflowNotOpen = "LavaFlow isn't available for this project yet"
	lavaflowNoPlan  = "LavaFlow needs a plan that includes it"
)

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
	{"lavaflow", "approve", "00000000-0000-4000-8000-000000000000", "--yes", "--json"},
}

// TestAPIE2ECloudLavaFlow runs LavaFlow's commands against a fresh project the
// way a user does. The project has no issues and no connected repository, so
// no run starts and nothing can open a pull request; Kong/volcano-hosting's
// cloud E2E covers runs.
//
// LavaFlow is open to a list of accounts, on plans that include it. A fresh
// account is not on the list, or its plan doesn't include LavaFlow, so every
// command says which and exits nonzero. Where the environment opens LavaFlow
// to every account, the round trip runs instead: settings saved by one command
// read back by the next, and a project access token reads but can't change
// them.
func TestAPIE2ECloudLavaFlow(t *testing.T) {
	env := setupAPIE2E(t, "cloud-lavaflow")
	env.loginAndUse(t)

	probe := env.runCloudCLI(t, "lavaflow", "status")
	for _, refusal := range []string{lavaflowNotOpen, lavaflowNoPlan} {
		if strings.Contains(probe.output, refusal) {
			for _, command := range lavaflowCommands {
				env.runCloudCLI(t, command...).requireFailure(t, refusal)
			}
			return
		}
	}

	// Registered after the project's own cleanup, so it runs first: LavaFlow is
	// off before the project is deleted, even when an assertion below fails.
	t.Cleanup(func() { env.runCloudCLI(t, "lavaflow", "disable").requireSuccess(t, "LavaFlow is off") })

	env.runCloudCLI(t, "lavaflow", "status").requireSuccess(t, "LavaFlow: off", "Repository: none connected",
		"Turn it on with volcano cloud lavaflow enable")
	env.runCloudCLI(t, "lavaflow", "enable", "--yes").requireSuccess(t,
		"LavaFlow sends this project's logs and source code to an AI service that Volcano operates.", "LavaFlow is on")
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
	requireAPIE2ELavaFlowMergeMode(t, env)
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
	env.runCloudCLIWithEnv(t, projectTokenEnv, "lavaflow", "approve", "00000000-0000-4000-8000-000000000000", "--yes").
		requireFailure(t, "approving a LavaFlow merge needs a person's session")
}

// requireAPIE2ELavaFlowMergeMode turns on merging where it's available and
// saves each merge mode, reading it back; where it isn't, autonomy merge is
// refused as unavailable. Either way the project ends at open_pr, and a run
// that doesn't exist can't be approved.
func requireAPIE2ELavaFlowMergeMode(t *testing.T, env *apiE2E) {
	t.Helper()
	unknownRun := "00000000-0000-4000-8000-000000000000"
	env.runCloudCLI(t, "lavaflow", "approve", unknownRun, "--yes").requireFailure(t, unknownRun)

	probe := env.runCloudCLI(t, "lavaflow", "settings", "set", "--autonomy", "merge")
	if probe.code != 0 {
		probe.requireFailure(t, "LavaFlow merging isn't available for this project yet")
		return
	}
	human := string(apiclient.LavaflowHumanMergeMode)
	defer env.runCloudCLI(t, "lavaflow", "settings", "set", "--autonomy", "open_pr", "--merge-mode", human).requireSuccess(t)
	for mode, want := range map[string]string{
		"risk_based": "risk-based",
		"auto":       "automatic",
		human:        "a person approves each merge",
	} {
		env.runCloudCLI(t, "lavaflow", "settings", "set", "--merge-mode", mode).requireSuccess(t, "Merging: "+want)
		settings := env.runCloudCLI(t, "lavaflow", "settings", "--json")
		settings.requireSuccess(t)
		var saved struct {
			MergePolicy struct {
				Mode string `json:"mode"`
			} `json:"merge_policy"`
		}
		if err := json.Unmarshal([]byte(settings.stdout), &saved); err != nil {
			t.Fatalf("settings --json did not emit JSON: %v\n%s", err, settings.output)
		}
		if saved.MergePolicy.Mode != mode {
			t.Fatalf("settings did not read back merge mode %q: %+v", mode, saved)
		}
	}
}
