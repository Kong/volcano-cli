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
	requireAPIE2ELavaFlowMergeRefusesATokenThatNeverExpires(t, env)
	requireAPIE2ELavaFlowUngatedWorkflowsRefuseATokenThatNeverExpires(t, env)
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
	env.runCloudCLIWithEnv(t, projectTokenEnv, "lavaflow", "settings", "set", "--allow-ungated-workflows", "false").
		requireFailure(t, "changing LavaFlow's settings needs a person's session")
}

// requireAPIE2ELavaFlowMergeRefusesATokenThatNeverExpires asks for autonomy
// merge with --yes and --json as the owner. The lane's owner token comes from
// the management API, which mints tokens that never expire, so after the CLI
// prints the merge warning, Volcano refuses the change with person_required in
// its own words (exit 5). Where merging isn't available for the project, it's
// refused as unavailable instead. Turning merge on, with a token from
// `volcano login`, is covered by QA's staging gate, not here.
func requireAPIE2ELavaFlowMergeRefusesATokenThatNeverExpires(t *testing.T, env *apiE2E) {
	t.Helper()
	// Should Volcano save merge anyway, the project still goes back to
	// diagnose_only before the next check.
	defer func() {
		env.runCloudCLI(t, "lavaflow", "settings", "set", "--autonomy", "diagnose_only").
			requireSuccess(t, "LavaFlow's settings are saved")
	}()

	merging := env.runCloudCLI(t, "lavaflow", "settings", "set", "--autonomy", "merge", "--yes", "--json")
	if strings.Contains(merging.output, "LavaFlow merging isn't available for this project yet") {
		merging.requireFailure(t)
		return
	}
	requireRefusedForATokenThatNeverExpires(t, merging, "run your CI with the secrets your pull request workflows get")
}

// requireAPIE2ELavaFlowUngatedWorkflowsRefuseATokenThatNeverExpires asks to
// allow ungated workflows with --yes and --json as the owner. As for merge,
// the lane's owner token never expires, so after the CLI prints the warning,
// Volcano refuses the change with person_required in its own words (exit 5).
// Turning them off takes any of the owner's credentials, so the deferred
// reset saves and prints them as not allowed. Allowing them, with a token
// from `volcano login`, is covered by QA's staging gate, not here.
func requireAPIE2ELavaFlowUngatedWorkflowsRefuseATokenThatNeverExpires(t *testing.T, env *apiE2E) {
	t.Helper()
	defer func() {
		env.runCloudCLI(t, "lavaflow", "settings", "set", "--allow-ungated-workflows", "false").
			requireSuccess(t, "Ungated workflows: not allowed")
	}()

	allowing := env.runCloudCLI(t, "lavaflow", "settings", "set", "--allow-ungated-workflows", "true", "--yes", "--json")
	requireRefusedForATokenThatNeverExpires(t, allowing,
		"Turn it on only if those workflows keep secrets away from untrusted code")
}

// requireRefusedForATokenThatNeverExpires checks a change the CLI warned
// about, then Volcano refused with person_required because the token never
// expires: the CLI keeps Volcano's words, which name `volcano login`, and
// exits 5.
func requireRefusedForATokenThatNeverExpires(t *testing.T, refused cliResult, warning string) {
	t.Helper()
	refused.requireFailure(t, warning, "LavaFlow's settings weren't changed: ", "never expires", "`volcano login`")
	if refused.code != 5 {
		t.Fatalf("the refused change exited %d, want 5:\n%s", refused.code, redactCredentials(refused.output))
	}
}
