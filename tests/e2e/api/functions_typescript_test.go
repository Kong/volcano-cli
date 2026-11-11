package api

import (
	"path/filepath"
	"testing"
)

// TestAPIE2ECloudFunctionsTypeScript deploys TypeScript handlers with no
// compiled JavaScript, which the Volcano build compiles: a single file, and a
// directory whose ES module imports a file through its tsconfig paths.
func TestAPIE2ECloudFunctionsTypeScript(t *testing.T) {
	env := setupAPIE2E(t, "cloud-functions-ts")
	writeAPIE2ETypeScriptFunctions(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "functions", "deploy", "--all").requireSuccess(t,
		"Found 2 function(s)",
		"(detected from .ts)",
		"(detected from index.ts)",
		"2/2 functions deployment started",
		"Deployed ts-file (new",
		"Deployed ts-dir (new")

	for name, greeting := range map[string]string{
		"ts-file": `"greeting":"hello from a typescript file"`,
		"ts-dir":  `"greeting":"hello from a typescript directory"`,
	} {
		env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", name)
		env.waitForCloudCLIContains(t, apiE2EFunctionConvergenceTimeout, greeting, "functions", "invoke", name, "--json")
	}
}

func writeAPIE2ETypeScriptFunctions(t *testing.T, projectDir string) {
	t.Helper()
	functionsDir := filepath.Join(projectDir, "volcano", "functions")
	writeAPIE2EFile(t, filepath.Join(functionsDir, "ts-file.ts"), `
interface Response {
  statusCode: number;
  body: string;
}

export const handler = async (): Promise<Response> => ({
  statusCode: 200,
  body: JSON.stringify({ greeting: "hello from a typescript file" }),
});
`)
	writeAPIE2EFile(t, filepath.Join(functionsDir, "ts-dir", "package.json"), `{"name":"ts-dir","private":true,"type":"module"}`)
	writeAPIE2EFile(t, filepath.Join(functionsDir, "ts-dir", "tsconfig.json"), `{"compilerOptions":{"baseUrl":".","paths":{"@lib/*":["lib/*"]}}}`)
	writeAPIE2EFile(t, filepath.Join(functionsDir, "ts-dir", "lib", "greeting.ts"), `
export const greeting = (place: string): string => "hello from a typescript " + place;
`)
	writeAPIE2EFile(t, filepath.Join(functionsDir, "ts-dir", "index.ts"), `
import { greeting } from "@lib/greeting";

export const handler = async (): Promise<{ statusCode: number; body: string }> => ({
  statusCode: 200,
  body: JSON.stringify({ greeting: greeting("directory") }),
});
`)
}
