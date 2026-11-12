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

// requireLocalModeDeploysJSXFunctions deploys .tsx handlers, which local mode
// compiles with their JSX as the cloud build does: a single file whose pragma
// picks its JSX factory, and a directory whose tsconfig.json does. Neither
// needs React, so no package installs.
func requireLocalModeDeploysJSXFunctions(t *testing.T, binary string, env []string) {
	t.Helper()
	dir := localModeE2EProjectDir(t)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "tsx-file.tsx"), `
/** @jsxRuntime classic */
/** @jsx h */
const h = (tag: string, _props: Record<string, unknown> | null, ...children: unknown[]): string =>
  tag + ":" + children.join("");

export const handler = async () => ({
  statusCode: 200,
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ rendered: <p>hello from a jsx file</p> }),
});
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "tsx-dir", "tsconfig.json"), `{"compilerOptions":{"jsx":"react","jsxFactory":"h"}}`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "tsx-dir", "h.ts"), `
type Component = (props: Record<string, unknown>) => string;

export const h = (tag: string | Component, props: Record<string, unknown> | null, ...children: unknown[]): string =>
  typeof tag === "function" ? tag({ ...props, children }) : tag + ":" + children.join("");
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "tsx-dir", "greeting.tsx"), `
import { h } from "./h";

export const Greeting = ({ place }: { place: string }): string => <p>hello from a jsx {place}</p>;
`)
	writeLocalModeE2EFile(t, dir, filepath.Join("volcano", "functions", "tsx-dir", "index.tsx"), `
import { h } from "./h";
import { Greeting } from "./greeting";

export const handler = async () => ({
  statusCode: 200,
  headers: { "content-type": "application/json" },
  body: JSON.stringify({ rendered: <Greeting place="directory" /> }),
});
`)

	output := runVolcanoLocalModeE2E(t, binary, env, dir, "functions", "deploy", "--all")
	requireContains(t, output, "Found 2 function(s)")
	requireContains(t, output, "(detected from .tsx)")
	requireContains(t, output, "(detected from index.tsx)")
	requireContains(t, output, "2/2 functions deployment started")

	info := fetchVolcanoLocalModeE2EInfo(t, env)
	for name, rendered := range map[string]string{
		"tsx-file": `"rendered":"p:hello from a jsx file"`,
		"tsx-dir":  `"rendered":"p:hello from a jsx directory"`,
	} {
		waitForVolcanoLocalModeE2EInvokeContains(t, info, waitForVolcanoLocalModeE2EFunctionID(t, info, name), rendered)
	}
}
