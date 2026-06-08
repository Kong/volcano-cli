package context

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
)

func TestContextUseCreatesPresetAndSetsDefault(t *testing.T) {
	setContextTestHome(t)

	out, err := executeContextCommand(t, "use", "production")
	require.NoError(t, err)
	assert.Contains(t, out, "Now using context: prod")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, config.ContextProd, cfg.DefaultContext)
	assert.Equal(t, "https://api.volcano.dev", cfg.ResolvedContext(config.ContextProd).APIBaseURL)
}

func TestContextSetCustom(t *testing.T) {
	setContextTestHome(t)

	_, err := executeContextCommand(t, "set", "qa", "--api-url", "https://qa.example", "--device-client-id", "devcli_custom")
	require.NoError(t, err)

	cfg, err := config.Load()
	require.NoError(t, err)
	ctx := cfg.ResolvedContext("qa")
	assert.Equal(t, "https://qa.example", ctx.APIBaseURL)
	assert.Equal(t, "devcli_custom", ctx.DeviceClientID)
}

func TestContextListShowsBuiltInsAndActive(t *testing.T) {
	setContextTestHome(t)
	cfg := config.Default()
	cfg.SetDefaultContext(config.ContextStage)
	require.NoError(t, cfg.Save())

	out, err := executeContextCommand(t, "list")
	require.NoError(t, err)
	assert.Contains(t, out, "dev")
	assert.Contains(t, out, "stage")
	assert.Contains(t, out, "prod")
	assert.Contains(t, out, "*         stage")
}

func TestContextDeleteRejectsBuiltIn(t *testing.T) {
	setContextTestHome(t)

	_, err := executeContextCommand(t, "delete", "dev")
	require.ErrorContains(t, err, "cannot delete built-in context")
}

func executeContextCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := New()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setContextTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_CONTEXT", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}
