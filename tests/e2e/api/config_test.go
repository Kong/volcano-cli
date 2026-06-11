package api

import "testing"

func TestAPIE2ECloudConfig(t *testing.T) {
	env := setupAPIE2E(t, "cloud-config")
	writeAPIE2EBaseProject(t, env.projectDir)

	env.loginAndUse(t)
	env.runCloudCLI(t, "functions", "deploy", "-f", "hello").requireSuccess(t, "1/1 functions deployment started")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "functions", "delete", "hello", "--yes")
	})

	configBucket := "cli-e2e-config-" + apiE2ESuffix(t)
	writeAPIE2EConfig(t, env.projectDir, configBucket, "hello")
	env.runCloudCLI(t, "config", "deploy").requireSuccess(t, "Configuration deployed", "Buckets:", "Policies:", "Functions:")
	env.runCloudCLI(t, "storage", "bucket", "get", configBucket).requireSuccess(t, configBucket, "8.0 KiB", "text/plain")
	env.runCloudCLI(t, "storage", "policy", "get", configBucket, "config-read").requireSuccess(t, "config-read", "SELECT", "true")
	env.runCloudCLI(t, "functions", "get", "hello").requireSuccess(t, "Visibility: public")
	t.Cleanup(func() {
		_ = env.runCloudCLI(t, "storage", "bucket", "delete", configBucket, "--yes")
	})
}
