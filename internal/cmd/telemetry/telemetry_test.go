package telemetry

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
	"github.com/Kong/volcano-cli/internal/telemetry"
)

func runCmd(t *testing.T, args ...string) (string, string) {
	t.Helper()
	cmd := New()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	require.NoError(t, cmd.Execute())
	return stdout.String(), stderr.String()
}

func savedState(t *testing.T) config.TelemetryConfig {
	t.Helper()
	cfg, err := config.Load()
	require.NoError(t, err)
	require.NotNil(t, cfg.Telemetry)
	return *cfg.Telemetry
}

func TestTelemetryCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(telemetry.EnvDisabled, "")
	require.NoError(t, config.Update(func(cfg *config.Config) (bool, error) {
		cfg.UserToken = "keep-me"
		cfg.Telemetry = &config.TelemetryConfig{DeviceID: "d"}
		return true, nil
	}))

	out, _ := runCmd(t)
	assert.Contains(t, out, "Telemetry is enabled.")

	out, _ = runCmd(t, "disable")
	assert.Equal(t, "Telemetry is disabled.\n", out)
	assert.Equal(t, config.TelemetryConfig{DeviceID: "d", Disabled: true, NoticeShown: true}, savedState(t))

	out, _ = runCmd(t, "status")
	assert.Contains(t, out, "Telemetry is disabled.")

	out, _ = runCmd(t, "enable")
	assert.Equal(t, "Telemetry is enabled.\n", out)
	assert.False(t, savedState(t).Disabled)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, "keep-me", cfg.UserToken, "the rest of config.json is kept")
}

func TestTelemetryCommand_EnvOptOut(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(telemetry.EnvDisabled, "1")

	out, _ := runCmd(t, "status")
	assert.Equal(t, "Telemetry is disabled by VOLCANO_TELEMETRY_DISABLED.\n", out)

	_, stderr := runCmd(t, "enable")
	assert.Contains(t, stderr, "VOLCANO_TELEMETRY_DISABLED is set")
}

func TestTelemetryCommandsAreNeverRecorded(t *testing.T) {
	cmd := New()
	assert.NotEmpty(t, cmd.Annotations[telemetry.SkipAnnotation])
	flush, _, err := cmd.Find([]string{"flush"})
	require.NoError(t, err)
	assert.True(t, flush.Hidden)
}
