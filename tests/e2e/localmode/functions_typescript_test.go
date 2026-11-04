package localmode

import (
	"path/filepath"
	"testing"
)

// requireLocalModeDeploysTypeScriptFunctions deploys TypeScript handlers with
// no compiled JavaScript, which local mode compiles as the cloud build does: a
// single file, and a directory whose ES module imports a file through its
// tsconfig paths.
func requireLocalModeDeploysTypeScriptFunctions(t *testing.T, binary string, env []string) {
	t.Helper()
	dir := localModeE2EProjectDir(t)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "ts-file.ts"), `
interface Response {
  statusCode: number;
  headers: Record<string, string>;
  body: string;
}

export const handler = async (): Promise<Response> => ({
  statusCode: 200,
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ greeting: "hello from a typescript file" }),
});
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "ts-dir", "package.json"), `{"name":"ts-dir","private":true,"type":"module"}`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "ts-dir", "tsconfig.json"), `{"compilerOptions":{"baseUrl":".","paths":{"@lib/*":["lib/*"]}}}`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "ts-dir", "lib", "greeting.ts"), `
export const greeting = (place: string): string => "hello from a typescript " + place;
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "ts-dir", "index.ts"), `
import { greeting } from "@lib/greeting";

export const handler = async (): Promise<{ statusCode: number; headers: Record<string, string>; body: string }> => ({
  statusCode: 200,
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ greeting: greeting("directory") }),
});
`)

	output := runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "deploy", "--all")
	requireContains(t, output, "Found 2 function(s)")
	requireContains(t, output, "(detected from .ts)")
	requireContains(t, output, "(detected from index.ts)")
	requireContains(t, output, "2/2 functions deployment started")

	info := fetchVolcanoLocalModeE2EInfo(t, env)
	for name, greeting := range map[string]string{
		"ts-file": `"greeting":"hello from a typescript file"`,
		"ts-dir":  `"greeting":"hello from a typescript directory"`,
	} {
		waitForVolcanoLocalModeE2EInvokeContains(t, info, waitForVolcanoLocalModeE2EFunctionID(t, info, name), greeting)
	}
}
