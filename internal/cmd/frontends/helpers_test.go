package frontends

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
)

const (
	frontendProjectID    = "22222222-2222-4222-8222-222222222222"
	frontendID           = "55555555-5555-4555-8555-555555555555"
	frontendDeploymentID = "66666666-6666-4666-8666-666666666666"
)

func executeFrontendsCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setFrontendCommandTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveFrontendCommandTestConfig(t *testing.T) {
	t.Helper()
	cfg := &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   frontendProjectID,
			Name: "Beta",
		},
	}
	require.NoError(t, cfg.Save())
}

func writeFrontendCommandJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}

func frontendCommandPayload(id, name string) map[string]any {
	return map[string]any{
		"app_root":              "apps/web",
		"created_at":            "2026-05-20T00:00:00Z",
		"current_deployment_id": frontendDeploymentID,
		"deployed_regions":      []string{"aws-us-east-1"},
		"framework":             "nextjs",
		"id":                    id,
		"name":                  name,
		"project_id":            frontendProjectID,
		"site_url":              "https://" + name + ".frontends.volcano.dev/",
		"status":                "ready",
		"updated_at":            "2026-05-20T00:00:00Z",
	}
}
