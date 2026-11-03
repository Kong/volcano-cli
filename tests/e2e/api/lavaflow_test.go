package api

import (
	"encoding/json"
	"strings"
	"testing"
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
	requireAPIE2ELavaFlowMerging(t, env)
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
	env.runCloudCLIWithEnv(t, projectTokenEnv, "lavaflow", "settings", "set", "--autonomy", "merge", "--yes").
		requireFailure(t, "changing LavaFlow's settings needs a person's session")
}

// requireAPIE2ELavaFlowMerging turns on autonomy merge with --yes and
// --json, then saves a human review path, reading both back. Where merging
// isn't available for the project, or the owner's GitHub account can't push
// to its repository, autonomy merge is refused instead. Either way the project
// ends at diagnose_only with no human review paths.
func requireAPIE2ELavaFlowMerging(t *testing.T, env *apiE2E) {
	t.Helper()
	type settings struct {
		Autonomy    string `json:"autonomy"`
		MergePolicy struct {
			HumanReviewPaths []string `json:"human_review_paths"`
		} `json:"merge_policy"`
	}
	decode := func(result cliResult) settings {
		var saved settings
		if err := json.Unmarshal([]byte(result.stdout), &saved); err != nil {
			t.Fatalf("lavaflow settings --json did not emit JSON: %v\n%s", err, result.output)
		}
		return saved
	}

	merging := env.runCloudCLI(t, "lavaflow", "settings", "set", "--autonomy", "merge", "--yes", "--json")
	if merging.code != 0 {
		if !strings.Contains(merging.output, "LavaFlow merging isn't available for this project yet") &&
			!strings.Contains(merging.output, "LavaFlow can't merge for this project") {
			t.Fatalf("settings set --autonomy merge failed for another reason:\n%s", merging.output)
		}
		return
	}
	defer env.runCloudCLI(t, "lavaflow", "settings", "set", "--autonomy", "diagnose_only", "--clear-human-review-paths").
		requireSuccess(t, "LavaFlow's settings are saved")
	merging.requireSuccess(t, "A fix it holds or stops is yours to review and merge on GitHub.")
	if saved := decode(merging); saved.Autonomy != "merge" {
		t.Fatalf("settings set --autonomy merge --json did not save autonomy merge: %+v", saved)
	}

	paths := env.runCloudCLI(t, "lavaflow", "settings", "set", "--human-review-path", "cli-e2e/**", "--json")
	paths.requireSuccess(t)
	if saved := decode(paths); len(saved.MergePolicy.HumanReviewPaths) != 1 || saved.MergePolicy.HumanReviewPaths[0] != "cli-e2e/**" {
		t.Fatalf("settings set --human-review-path did not save the path: %+v", saved)
	}
	read := env.runCloudCLI(t, "lavaflow", "settings", "--json")
	read.requireSuccess(t)
	if saved := decode(read); saved.Autonomy != "merge" || len(saved.MergePolicy.HumanReviewPaths) != 1 {
		t.Fatalf("settings did not read back autonomy merge and the human review path: %+v", saved)
	}
}
