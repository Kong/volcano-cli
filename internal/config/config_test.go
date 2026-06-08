package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSaveDeleteAndPermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &Config{
		UserToken: "file-token",
		UserID:    "user-123",
		CurrentProject: &ProjectConfig{
			ID:   "project-123",
			Name: "Project 123",
		},
	}
	require.NoError(t, cfg.Save())

	configPath, err := Path()
	require.NoError(t, err)

	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(defaultConfigFileMode), info.Mode().Perm())

	dirInfo, err := os.Stat(filepath.Dir(configPath))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(defaultConfigDirMode), dirInfo.Mode().Perm())

	loaded, err := Load()
	require.NoError(t, err)
	assert.Equal(t, cfg.UserToken, loaded.Token())
	assert.Equal(t, cfg.UserID, loaded.UserID)
	require.NotNil(t, loaded.CurrentProject)
	assert.Equal(t, cfg.CurrentProject.ID, loaded.CurrentProject.ID)
	assert.Equal(t, cfg.CurrentProject.Name, loaded.CurrentProject.Name)

	require.NoError(t, Delete())
	_, err = os.Stat(configPath)
	assert.True(t, os.IsNotExist(err), "config exists after delete: %v", err)

	empty, err := Load()
	require.NoError(t, err)
	assert.Empty(t, empty.UserToken)
	assert.Empty(t, empty.UserID)
	assert.Nil(t, empty.CurrentProject)
}

func TestContextPresetsAndDefaultSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envToken, "")
	t.Setenv(envProjectID, "")
	t.Setenv(envAPIURL, "")
	t.Setenv(envContext, "")
	t.Setenv(envFirstPartyDeviceID, "")

	cfg := Default()
	assert.Equal(t, ContextProd, cfg.ActiveContextName())
	assert.Equal(t, "https://api.volcano.dev", cfg.APIURL())
	clientID, err := cfg.DeviceClientID()
	require.NoError(t, err)
	assert.Equal(t, prodDeviceClientID, clientID)

	cfg.SetDefaultContext("production")
	assert.Equal(t, ContextProd, cfg.ActiveContextName())

	cfg.SetDefaultContext(ContextDev)
	assert.Equal(t, ContextDev, cfg.ActiveContextName())
	assert.Equal(t, "http://localhost:8000", cfg.APIURL())
	clientID, err = cfg.DeviceClientID()
	require.NoError(t, err)
	assert.Equal(t, devDeviceClientID, clientID)

	cfg.SetDefaultContext(ContextStage)
	assert.Equal(t, "https://api.staging.volcano.dev", cfg.APIURL())
	clientID, err = cfg.DeviceClientID()
	require.NoError(t, err)
	assert.Equal(t, devDeviceClientID, clientID)
}

func TestContextSelectionPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envAPIURL, "")
	t.Setenv(envContext, ContextStage)

	cfg := Default()
	cfg.SetDefaultContext(ContextDev)
	assert.Equal(t, ContextStage, cfg.ActiveContextName())
	assert.Equal(t, "https://api.staging.volcano.dev", cfg.APIURL())

	cfg.SetContextOverride(ContextProd)
	assert.Equal(t, ContextProd, cfg.ActiveContextName())
	assert.Equal(t, "https://api.volcano.dev", cfg.APIURL())
}

func TestCustomContextRequiresDeviceClientID(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envFirstPartyDeviceID, "")
	t.Setenv(envContext, "")

	cfg := Default()
	cfg.SetDefaultContext("custom")
	cfg.EnsureContext("custom").APIBaseURL = "https://custom.example"

	_, err := cfg.DeviceClientID()
	require.ErrorContains(t, err, "device_client_id is required")
}

func TestSaveOmitsRuntimeOnlyAPIURLOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &Config{
		APIBaseURL: "http://localhost:8000",
		UserToken:  "file-token",
	}
	require.NoError(t, cfg.Save())

	configPath, err := Path()
	require.NoError(t, err)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "http://localhost:8000")

	loaded, err := Load()
	require.NoError(t, err)
	assert.Empty(t, loaded.APIBaseURL)
	assert.Equal(t, "file-token", loaded.UserToken)
}

func TestSaveRepairsExistingConfigPermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	configPath, err := Path()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o755))
	require.NoError(t, os.WriteFile(configPath, []byte(`{"user_token":"old-token"}`), 0o644))

	cfg := &Config{UserToken: "new-token"}
	require.NoError(t, cfg.Save())

	info, err := os.Stat(configPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(defaultConfigFileMode), info.Mode().Perm())

	dirInfo, err := os.Stat(filepath.Dir(configPath))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(defaultConfigDirMode), dirInfo.Mode().Perm())

	loaded, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "new-token", loaded.UserToken)
}

func TestLoadMigratesLegacyConfigUsingAPIURLEnv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envContext, "")
	t.Setenv(envAPIURL, stageAPIURL)

	configPath, err := Path()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o700))
	require.NoError(t, os.WriteFile(configPath, []byte(`{"user_token":"stage-token"}`), 0o600))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ContextStage, cfg.DefaultContext)
	assert.Equal(t, "stage-token", cfg.ResolvedContext(ContextStage).UserToken)
	assert.Empty(t, cfg.ResolvedContext(ContextProd).UserToken)
}

func TestLoadMigratesLegacyConfigUsingContextEnvBeforeAPIURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envContext, ContextDev)
	t.Setenv(envAPIURL, stageAPIURL)

	configPath, err := Path()
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o700))
	require.NoError(t, os.WriteFile(configPath, []byte(`{"user_token":"dev-token"}`), 0o600))

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, ContextDev, cfg.DefaultContext)
	assert.Equal(t, "dev-token", cfg.ResolvedContext(ContextDev).UserToken)
	assert.Empty(t, cfg.ResolvedContext(ContextStage).UserToken)
}

func TestEnvPrecedence(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envToken, "env-token")
	t.Setenv(envProjectID, "env-project")
	t.Setenv(envAPIURL, "https://env.example")
	t.Setenv(envFirstPartyDeviceID, "env-device-client")

	originalAPIURL := compiledDefaultAPIURL
	originalDeviceClientID := compiledFirstPartyDeviceClientID
	compiledDefaultAPIURL = "https://compiled.example"
	compiledFirstPartyDeviceClientID = "compiled-device-client"
	t.Cleanup(func() {
		compiledDefaultAPIURL = originalAPIURL
		compiledFirstPartyDeviceClientID = originalDeviceClientID
	})

	cfg := &Config{
		APIBaseURL: "https://file.example",
		UserToken:  "file-token",
		CurrentProject: &ProjectConfig{
			ID:   "file-project",
			Name: "File Project",
		},
	}

	assert.Equal(t, "env-token", cfg.Token())
	assert.Equal(t, "env-project", cfg.ProjectID())
	assert.Equal(t, "https://env.example", cfg.APIURL())

	got, err := FirstPartyDeviceClientID()
	require.NoError(t, err)
	assert.Equal(t, "env-device-client", got)
}

func TestRuntimeAPIURLPrecedesCompiledDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envAPIURL, "")

	originalAPIURL := compiledDefaultAPIURL
	compiledDefaultAPIURL = "https://compiled.example"
	t.Cleanup(func() {
		compiledDefaultAPIURL = originalAPIURL
	})

	cfg := &Config{APIBaseURL: "http://localhost:8000"}
	assert.Equal(t, "http://localhost:8000", cfg.APIURL())
}

func TestEnvAPIURLOverridesCompiledDefault(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envAPIURL, "https://env.example")

	originalAPIURL := compiledDefaultAPIURL
	compiledDefaultAPIURL = "https://compiled.example"
	t.Cleanup(func() {
		compiledDefaultAPIURL = originalAPIURL
	})

	cfg := &Config{}
	assert.Equal(t, "https://env.example", cfg.APIURL())
}

func TestIgnoreEnvUsesConfigValues(t *testing.T) {
	t.Setenv(envToken, "env-token")
	t.Setenv(envProjectID, "env-project")
	t.Setenv(envAPIURL, "https://env.example")

	cfg := &Config{
		APIBaseURL: "http://localhost:8000",
		UserToken:  "file-token",
		CurrentProject: &ProjectConfig{
			ID:   "file-project",
			Name: "File Project",
		},
		IgnoreEnv: true,
	}

	assert.Equal(t, "file-token", cfg.Token())
	assert.Equal(t, "file-project", cfg.ProjectID())
	assert.Equal(t, "http://localhost:8000", cfg.APIURL())
}

func TestProdPresetAndCompiledDeviceClientFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(envAPIURL, "")
	t.Setenv(envFirstPartyDeviceID, "")

	originalAPIURL := compiledDefaultAPIURL
	originalDeviceClientID := compiledFirstPartyDeviceClientID
	compiledDefaultAPIURL = "https://compiled.example"
	compiledFirstPartyDeviceClientID = "compiled-device-client"
	t.Cleanup(func() {
		compiledDefaultAPIURL = originalAPIURL
		compiledFirstPartyDeviceClientID = originalDeviceClientID
	})

	cfg := &Config{}
	assert.Equal(t, "https://api.volcano.dev", cfg.APIURL())

	got, err := FirstPartyDeviceClientID()
	require.NoError(t, err)
	assert.Equal(t, "compiled-device-client", got)

	compiledFirstPartyDeviceClientID = ""
	_, err = FirstPartyDeviceClientID()
	require.Error(t, err)
}

func TestRequireAuth(t *testing.T) {
	t.Setenv(envToken, "")

	cfg := &Config{}
	require.ErrorIs(t, cfg.RequireAuth(), ErrNotAuthenticated)

	cfg.UserToken = "file-token"
	require.NoError(t, cfg.RequireAuth())

	t.Setenv(envToken, "env-token")
	cfg.UserToken = ""
	require.NoError(t, cfg.RequireAuth())
}

func TestRequireProject(t *testing.T) {
	t.Setenv(envProjectID, "")

	cfg := &Config{}
	require.ErrorIs(t, cfg.RequireProject(), ErrNoProjectSelected)

	cfg.CurrentProject = &ProjectConfig{ID: "file-project", Name: "File Project"}
	require.NoError(t, cfg.RequireProject())

	t.Setenv(envProjectID, "env-project")
	cfg.CurrentProject = nil
	require.NoError(t, cfg.RequireProject())
}
