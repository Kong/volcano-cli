package api

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The project "volcano init nextjs" generates has its package.json at the root
// and builds web/ from there, so it is deployed from the root, as a user would
// after following init's next steps.
func TestAPIE2ECloudFrontendsInitNextjs(t *testing.T) {
	env := setupAPIE2E(t, "cloud-frontends-init")

	env.loginAndUse(t)
	env.runCLI(t, "init", "nextjs").requireSuccess(t, "Run: npm install")
	runAPIE2ENpmInstall(t, env.projectDir)
	frontend := "cli-e2e-init-" + apiE2ESuffix(t)
	env.runCloudCLI(t, "frontends", "deploy", "--name", frontend).requireSuccess(t, "deployment started")
	active := env.waitForCloudCLIContains(t, apiE2EFrontendDeploymentTimeout, "Status: active", "frontends", "get", frontend)
	waitForAPIE2EFrontendContent(t, cliOutputField(active.output, "Site URL:"), "Hello from Volcano")
	firstDeployment := cliOutputField(active.output, "Current Deployment:")

	// The platform wraps a next.config.ts with its own config at build time.
	const configMarker = "Volcano CLI E2E next.config.ts"
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "web", "next.config.ts"), `import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  env: {
    CLI_E2E_CONFIG_MARKER: "`+configMarker+`",
  },
};

export default nextConfig;
`)
	writeAPIE2EFile(t, filepath.Join(env.projectDir, "web", "app", "page.js"), `export default function HomePage() {
  return <main>{process.env.CLI_E2E_CONFIG_MARKER}</main>;
}
`)
	env.runCloudCLI(t, "frontends", "deploy", "--name", frontend).requireSuccess(t, "deployment started")
	active = env.waitForFrontendDeployment(t, frontend, firstDeployment)
	waitForAPIE2EFrontendContent(t, cliOutputField(active.output, "Site URL:"), configMarker)

	env.runCloudCLI(t, "frontends", "delete", frontend, "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No frontends deployed", "frontends", "list")
}

// runAPIE2ENpmInstall leaves the package-lock.json a deploy uploads; the
// archive excludes node_modules.
func runAPIE2ENpmInstall(t *testing.T, dir string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), apiE2ECommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npm", "install", "--no-audit", "--no-fund")
	cmd.Dir = dir
	// The suite's proxy reaches frontends only and refuses the npm registry.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return strings.EqualFold(name, "HTTPS_PROXY") || strings.EqualFold(name, "HTTP_PROXY")
	})
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("npm install: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(dir, "package-lock.json")); err != nil {
		t.Fatalf("npm install left no package-lock.json: %v", err)
	}
}

// The notes example's web/ imports volcano/_shared, outside the Next.js app.
func TestAPIE2ECloudFrontendsInitNextjsNotes(t *testing.T) {
	env := setupAPIE2E(t, "cloud-frontends-notes")

	env.loginAndUse(t)
	env.runCLI(t, "init", "nextjs", "--example", "notes").requireSuccess(t, "Run: npm install")
	runAPIE2ENpmInstall(t, env.projectDir)
	frontend := "cli-e2e-notes-" + apiE2ESuffix(t)
	env.runCloudCLI(t, "frontends", "deploy", "--name", frontend).requireSuccess(t, "deployment started")
	active := env.waitForCloudCLIContains(t, apiE2EFrontendDeploymentTimeout, "Status: active", "frontends", "get", frontend)
	waitForAPIE2EFrontendContent(t, cliOutputField(active.output, "Site URL:"), "Volcano Notes Demo")

	env.runCloudCLI(t, "frontends", "delete", frontend, "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No frontends deployed", "frontends", "list")
}
