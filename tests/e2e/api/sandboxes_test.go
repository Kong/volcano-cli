package api

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestAPIE2ECloudSandboxes(t *testing.T) {
	env := setupAPIE2E(t, "cloud-sandboxes")
	env.loginAndUse(t)
	template := uuid.NewString()
	t.Cleanup(func() {
		env.runCloudCLI(t, "sandboxes", "templates", "delete", template, "--yes").requireSuccess(t, template)
	})
	key := uuid.NewString()
	contextDir := filepath.Join(env.projectDir, "sandbox")
	writeAPIE2EFile(t, filepath.Join(contextDir, "Dockerfile"), "RUN dnf install -y python3.12 && dnf clean all\nRUN printf custom-image > /image-version\nCMD [\"python3.12\", \"-m\", \"http.server\", \"8080\", \"--bind\", \"0.0.0.0\"]\n")
	deployArgs := []string{"sandboxes", "templates", "deploy", "cli-custom", "--template", template, "--request-id", key, "--path", contextDir, "--ports", "8080", "--json"}
	deployed := env.runCloudCLI(t, deployArgs...)
	deployed.requireSuccess(t, "deployment")
	var result struct {
		Deployment struct {
			ID string `json:"id"`
		} `json:"deployment"`
	}
	if err := json.Unmarshal([]byte(deployed.stdout), &result); err != nil {
		t.Fatalf("decode sandbox deployment response: %v", err)
	}
	if result.Deployment.ID == "" {
		t.Fatal("sandbox deployment response is missing deployment ID")
	}
	retried := env.runCloudCLI(t, deployArgs...)
	retried.requireSuccess(t, result.Deployment.ID)
	env.waitForCloudCLIContains(t, apiE2EFrontendDeploymentTimeout, `"status":"active"`, "sandboxes", "deployments", "get", template, result.Deployment.ID, "--json")
	env.runCloudCLI(t, "sandboxes", "deployments", "list", template, "--json").requireSuccess(t, result.Deployment.ID)
	env.runCloudCLI(t, "sandboxes", "exec", "--template", template, "--region", "aws-us-east-1", "--", "cat", "/image-version").requireSuccess(t, "custom-image")
}
