package api

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAPIE2ECloudFrontends(t *testing.T) {
	env := setupAPIE2E(t, "cloud-frontends")

	env.loginAndUse(t)
	frontend := "cli-e2e-" + apiE2ESuffix(t)
	writeAPIE2EFrontend(t, env.projectDir)
	env.runCloudCLI(t, "frontends", "deploy", "--name", frontend, "--path", filepath.Join(env.projectDir, "web")).requireSuccess(t, "deployment started")
	env.runCloudCLI(t, "frontends", "list").requireSuccess(t, frontend)
	env.waitForCloudCLIContains(t, 15*time.Minute, "Status: active", "frontends", "get", frontend)
	env.runCloudCLI(t, "frontends", "redeploy", frontend).requireSuccess(t, "redeploy started")
	env.runCloudCLI(t, "frontends", "delete", frontend, "--yes").requireSuccess(t, "deletion started")
}
