package api

import (
	"encoding/json"
	"testing"
)

// TestAPIE2ECloudGit reads a fresh project's Git deployment runs and chooses
// the volcano-config.yaml sections a push applies, the way a user does.
//
// The project has no repository connected, so no push arrives and no run is
// recorded; Kong/volcano-hosting's git-deploy cloud E2E covers the runs a push
// records. The sections are saved without a repository, so their round trip
// runs here: chosen by one command and read back by the next, read but not
// chosen with a project access token.
func TestAPIE2ECloudGit(t *testing.T) {
	env := setupAPIE2E(t, "cloud-git")
	env.loginAndUse(t)

	env.runCLI(t, "git", "runs", "list").requireSuccess(t, "No Git deployment runs.")
	listed := env.runCLI(t, "git", "runs", "list", "--json")
	listed.requireSuccess(t)
	var page struct {
		Data    []json.RawMessage `json:"data"`
		HasMore bool              `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(listed.stdout), &page); err != nil {
		t.Fatalf("runs list --json did not emit JSON: %v\n%s", err, listed.output)
	}
	if len(page.Data) != 0 || page.HasMore {
		t.Fatalf("a project nothing was pushed to listed runs:\n%s", listed.output)
	}
	missing := env.runCLI(t, "git", "runs", "get", "00000000-0000-4000-8000-000000000000")
	missing.requireFailure(t, "this project has no Git deployment run with that ID")
	if missing.code != 4 {
		t.Fatalf("runs get for a missing run exited %d, want 4:\n%s", missing.code, missing.output)
	}

	env.runCLI(t, "git", "manifest-sections").requireSuccess(t, "Manifest sections: none")
	env.runCLI(t, "git", "manifest-sections", "set", "frontends", "--yes").requireSuccess(t,
		"A push now applies frontends from volcano-config.yaml",
		"No repository is connected, so no push applies them yet.")
	requireAPIE2EManifestSections(t, env, "frontends")

	requireAPIE2EManifestSectionsNeedAPerson(t, env)

	env.runCLI(t, "git", "manifest-sections", "set", "--none").
		requireSuccess(t, "A push now applies no volcano-config.yaml sections")
	requireAPIE2EManifestSections(t, env)
}

// requireAPIE2EManifestSections reads the sections back as JSON and checks
// they are exactly want.
func requireAPIE2EManifestSections(t *testing.T, env *apiE2E, want ...string) {
	t.Helper()
	read := env.runCLI(t, "git", "manifest-sections", "--json")
	read.requireSuccess(t)
	var settings struct {
		ManifestSections []string `json:"manifest_sections"`
	}
	if err := json.Unmarshal([]byte(read.stdout), &settings); err != nil {
		t.Fatalf("manifest-sections --json did not emit JSON: %v\n%s", err, read.output)
	}
	if len(settings.ManifestSections) != len(want) {
		t.Fatalf("manifest sections read back %q, want %q", settings.ManifestSections, want)
	}
	for i, section := range want {
		if settings.ManifestSections[i] != section {
			t.Fatalf("manifest sections read back %q, want %q", settings.ManifestSections, want)
		}
	}
}

// requireAPIE2EManifestSectionsNeedAPerson checks a project access token reads
// the sections and the runs but can't choose the sections, with the exit code
// of a refused credential.
func requireAPIE2EManifestSectionsNeedAPerson(t *testing.T, env *apiE2E) {
	t.Helper()
	name := "cli-e2e-git-" + apiE2ESuffix(t)
	created := env.runCloudCLI(t, "access-tokens", "create", name, "--scope", "full", "--json")
	_, secret := apiE2ECreatedAccessToken(t, created)
	t.Cleanup(func() {
		if revoked := env.runCloudCLI(t, "access-tokens", "revoke", name, "--yes"); revoked.code != 0 {
			t.Errorf("failed to revoke the access token this test minted:\n%s", redactCredentials(revoked.output))
		}
	})

	projectTokenEnv := []string{"HOME=" + t.TempDir(), "VOLCANO_TOKEN=" + secret}
	env.runCLIWithEnv(t, projectTokenEnv, "use", env.projectID).requireSuccess(t, "Now using project")
	env.runCLIWithEnv(t, projectTokenEnv, "git", "manifest-sections").requireSuccess(t, "Manifest sections: frontends")
	env.runCLIWithEnv(t, projectTokenEnv, "git", "runs", "list").requireSuccess(t, "No Git deployment runs.")

	refused := env.runCLIWithEnv(t, projectTokenEnv, "git", "manifest-sections", "set", "--none")
	refused.requireFailure(t, "choosing what a push applies is a person's decision: a project access token can't",
		"Unset VOLCANO_TOKEN and sign in with 'volcano login'")
	if refused.code != 5 {
		t.Fatalf("manifest-sections set with a project access token exited %d, want 5:\n%s",
			refused.code, redactCredentials(refused.output))
	}
}
