package api

import (
	"path/filepath"
	"testing"
)

func TestAPIE2ECloudStorage(t *testing.T) {
	env := setupAPIE2E(t, "cloud-storage")

	env.loginAndUse(t)
	bucket := "cli-e2e-" + apiE2ESuffix(t)
	objectPath := "greetings/hello.txt"
	storageToken := createAPIE2EServiceKey(t, env.apiURL, env.token, env.projectID, "cli-e2e-storage-"+apiE2ESuffix(t))
	env.runCLI(t, "storage", "bucket", "create", bucket, "--allowed-mime-type", "text/plain", "--file-size-limit", "4096").requireSuccess(t, bucket)
	t.Cleanup(func() {
		_ = env.runCLI(t, "storage", "bucket", "delete", bucket, "--yes")
	})
	env.runCLI(t, "storage", "bucket", "list").requireSuccess(t, bucket)
	env.runCLI(t, "storage", "bucket", "get", bucket).requireSuccess(t, bucket, "4.0 KiB", "text/plain")
	env.runCLI(t, "storage", "bucket", "update", bucket, "--allowed-mime-type", "text/plain", "--file-size-limit", "8192").requireSuccess(t, bucket, "8.0 KiB")
	env.runCLI(t, "storage", "policy", "create", bucket, "--name", "anon-read", "--operation", "SELECT", "--definition", "true").requireSuccess(t, "anon-read")
	env.runCLI(t, "storage", "policy", "list", bucket).requireSuccess(t, "anon-read")

	localObject := filepath.Join(env.projectDir, "hello.txt")
	writeAPIE2EFile(t, localObject, "hello from cli e2e\n")
	env.runCLIWithEnv(t, []string{"VOLCANO_TOKEN=" + storageToken}, "storage", "object", "upload", bucket, localObject, objectPath, "--content-type", "text/plain").requireSuccess(t, objectPath)
	env.runCLIWithEnv(t, []string{"VOLCANO_TOKEN=" + storageToken}, "storage", "object", "list", bucket).requireSuccess(t, objectPath)
	env.runCLIWithEnv(t, []string{"VOLCANO_TOKEN=" + storageToken}, "storage", "object", "download", bucket, objectPath, "-").requireSuccess(t, "hello from cli e2e")
	env.runCLIWithEnv(t, []string{"VOLCANO_TOKEN=" + storageToken}, "storage", "object", "delete", bucket, objectPath, "--yes").requireSuccess(t, "deleted")
	env.runCLI(t, "storage", "policy", "delete", bucket, "anon-read", "--yes").requireSuccess(t, "deleted")
	env.runCLI(t, "storage", "bucket", "delete", bucket, "--yes").requireSuccess(t, "deleted")
}
