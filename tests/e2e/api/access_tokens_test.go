package api

import (
	"strings"
	"testing"
	"time"
)

func TestAPIE2ESmokeAccessTokens(t *testing.T) {
	env := setupAPIE2E(t, "smoke-access-tokens")
	env.loginAndUse(t)

	name := "cli-e2e-" + apiE2ESuffix(t)
	expiresAt := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	created := env.runCloudCLI(t, "access-tokens", "create", name, "--scope", "full", "--expires-at", expiresAt)
	created.requireSuccess(t, "Access token '"+name+"' created", "cannot be retrieved again")
	secret := apiE2EAccessTokenSecret(t, created.output)

	env.runCloudCLI(t, "access-tokens", "list").requireSuccess(t, name, "full", "active")
	env.runCloudCLI(t, "access-tokens", "get", name, "--usage", "--days", "7").
		requireSuccess(t, "Name: "+name, "Requests:", "request(s) over 7 day(s)")
	env.runCloudCLI(t, "access-tokens", "usage", "--days", "7").
		requireSuccess(t, name, "request(s) across", "over 7 day(s)")

	// The secret is printed once, so it must never come back from a read.
	env.runCloudCLI(t, "access-tokens", "get", name).requireNotContains(t, secret)
	env.runCloudCLI(t, "access-tokens", "list", "--json").requireNotContains(t, secret)

	// Duplicate names are rejected by the API, not silently accepted.
	env.runCloudCLI(t, "access-tokens", "create", name).requireFailure(t, "failed to create access token")

	projectTokenEnv := []string{"VOLCANO_TOKEN=" + secret, "VOLCANO_PROJECT_ID=" + env.projectID}

	// The minted token authenticates project-scoped commands...
	env.runCloudCLIWithEnv(t, projectTokenEnv, "variables", "list").requireSuccess(t)
	// ...but not account-wide ones, and it says so instead of failing with a 403.
	env.runCLIWithEnv(t, projectTokenEnv, "projects", "list").requireFailure(t, "needs an account token")
	env.runCLIWithEnv(t, projectTokenEnv, "projects", "create", "cli-e2e-denied").requireFailure(t, "needs an account token")
	env.runCLIWithEnv(t, projectTokenEnv, "use", env.project).requireFailure(t, "needs an account token")
	// Nor can it mint or revoke credentials of its own.
	env.runCloudCLIWithEnv(t, projectTokenEnv, "access-tokens", "list").requireFailure(t, "needs an account token")
	// Usage is the exception the API makes, so a CI job can report what it
	// consumed with nothing but the credential it runs with.
	env.runCloudCLIWithEnv(t, projectTokenEnv, "access-tokens", "usage", "--days", "7").
		requireSuccess(t, "request(s) across", "over 7 day(s)")

	// Logging in with it needs the project named, then binds the CLI to it.
	loginHome := t.TempDir()
	env.runCLIWithEnv(t, []string{"HOME=" + loginHome}, "login", "--token", secret).
		requireFailure(t, "--project <project-id>")
	env.runCLIWithEnv(t, []string{"HOME=" + loginHome}, "login", "--token", secret, "--project", env.projectID).
		requireSuccess(t, "Logged in successfully", "Now using project: "+env.project)
	env.runCloudCLIWithEnv(t, []string{"HOME=" + loginHome}, "variables", "list").requireSuccess(t)

	env.runCloudCLI(t, "access-tokens", "revoke", name, "--yes").requireSuccess(t, "Access token '"+name+"' revoked")

	// Revoked tokens stop authenticating but keep their record and their history.
	env.runCloudCLIWithEnv(t, projectTokenEnv, "variables", "list").requireFailure(t)
	env.runCloudCLI(t, "access-tokens", "list").requireNotContains(t, name)
	env.runCloudCLI(t, "access-tokens", "list", "--include-revoked").requireSuccess(t, name, "revoked")
	env.runCloudCLI(t, "access-tokens", "usage").requireSuccess(t, name)
}

// apiE2EAccessTokenSecret pulls the plaintext secret out of `access-tokens
// create` output, which prints it on its own "Token:" line and nowhere else.
func apiE2EAccessTokenSecret(t *testing.T, output string) string {
	t.Helper()
	for line := range strings.SplitSeq(output, "\n") {
		secret, ok := strings.CutPrefix(strings.TrimSpace(line), "Token:")
		if !ok {
			continue
		}
		if secret = strings.TrimSpace(secret); secret != "" {
			return secret
		}
	}
	t.Fatalf("create output did not print a token secret:\n%s", output)
	return ""
}
