package api

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAPIE2ECloudSandboxes(t *testing.T) {
	env := setupAPIE2E(t, "cloud-sandboxes")
	env.loginAndUse(t)
	template := uuid.NewString()
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
	require.NoError(t, json.Unmarshal([]byte(deployed.stdout), &result))
	require.NotEmpty(t, result.Deployment.ID)
	retried := env.runCloudCLI(t, deployArgs...)
	retried.requireSuccess(t, result.Deployment.ID)
	env.waitForCloudCLIContains(t, apiE2EFrontendDeploymentTimeout, `"status":"active"`, "sandboxes", "deployments", "get", template, result.Deployment.ID, "--json")
	env.runCloudCLI(t, "sandboxes", "deployments", "list", template, "--json").requireSuccess(t, result.Deployment.ID)
	env.runCloudCLI(t, "sandboxes", "exec", "--template", template, "--region", "us-east-1", "--", "cat", "/image-version").requireSuccess(t, "custom-image")
	env.runCloudCLI(t, "sandboxes", "templates", "delete", template, "--yes").requireSuccess(t, template)
}
