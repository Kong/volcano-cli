package api

import (
	"path/filepath"
	"testing"
)

func TestAPIE2ESmokeFunctions(t *testing.T) {
	env := setupAPIE2E(t, "smoke-functions")

	env.loginAndUse(t)
	env.runCLI(t, "functions", "list").requireSuccess(t, "No functions")
}

func TestAPIE2ECloudFunctions(t *testing.T) {
	env := setupAPIE2E(t, "cloud-functions")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCLI(t, "functions", "deploy", "--all").requireSuccess(t, "functions deployment started")
	env.runCLI(t, "functions", "deploy", "-f", "hello").requireSuccess(t, "1/1 functions deployment started")
	env.runCLI(t, "functions", "deploy", "-f", filepath.Join("volcano", "functions", "hello.js")).requireSuccess(t, "1/1 functions deployment started")
	env.runCLI(t, "functions", "list").requireSuccess(t, "hello")
	env.runCLI(t, "functions", "get", "hello").requireSuccess(t, "Name: hello", "Visibility: private")
	env.runCLI(t, "functions", "update", "hello", "--public").requireSuccess(t, "visibility set to public")
	env.runCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: public")
	env.runCLI(t, "functions", "update", "hello", "--private").requireSuccess(t, "visibility set to private")

	env.runCLI(t, "functions", "delete", "hello", "--yes").requireSuccess(t, "deletion started")
}
