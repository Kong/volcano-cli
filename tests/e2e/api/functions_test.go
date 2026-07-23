package api

import (
	"path/filepath"
	"testing"
)

func TestAPIE2ESmokeFunctions(t *testing.T) {
	env := setupAPIE2E(t, "smoke-functions")

	env.loginAndUse(t)
	env.runCloudCLI(t, "functions", "list").requireSuccess(t, "No functions")
}

func TestAPIE2ECloudFunctions(t *testing.T) {
	env := setupAPIE2E(t, "cloud-functions")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "functions", "deploy", "--all").requireSuccess(t, "functions deployment started")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	env.runCloudCLI(t, "functions", "deploy", "-f", "hello").requireSuccess(t, "1/1 functions deployment started")
	env.runCloudCLI(t, "functions", "deploy", "-f", filepath.Join("volcano", "functions", "hello.js")).requireSuccess(t, "1/1 functions deployment started")
	env.waitForCloudCLIContains(t, apiE2EFunctionDeploymentTimeout, "Status: active", "functions", "get", "hello")
	env.runCloudCLI(t, "functions", "list").requireSuccess(t, "hello")
	env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Name: hello", "Visibility: private")
	env.runCloudCLI(t, "functions", "update", "hello", "--public").requireSuccess(t, "visibility set to public")
	env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: public")
	env.runCloudCLI(t, "functions", "update", "hello", "--private").requireSuccess(t, "visibility set to private")

	env.runCloudCLI(t, "functions", "delete", "hello", "--yes").requireSuccess(t, "deletion started")
	env.waitForCloudCLIContains(t, apiE2EResourceDeleteTimeout, "No functions deployed", "functions", "list")
}
