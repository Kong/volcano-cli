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
	assert.Equal(t, cfg.UserToken, loaded.UserToken)
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

func TestSaveOmitsRuntimeOnlyAPIURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &Config{
		APIBaseURL: "http://localhost:8000",
		UserToken:  "file-token",
		AnonKey:    "local-anon-key",
		ServiceKey: "local-service-key",
	}
	require.NoError(t, cfg.Save())

	configPath, err := Path()
	require.NoError(t, err)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "api_url")
	assert.NotContains(t, string(data), "http://localhost:8000")
	assert.NotContains(t, string(data), "local-anon-key")
	assert.NotContains(t, string(data), "local-service-key")

	loaded, err := Load()
	require.NoError(t, err)
	assert.Empty(t, loaded.APIBaseURL)
	assert.Empty(t, loaded.AnonKey)
	assert.Empty(t, loaded.ServiceKey)
	assert.Equal(t, "file-token", loaded.UserToken)
}

func TestFunctionInvokeTokenPrefersServiceKey(t *testing.T) {
	cfg := &Config{
		UserToken:  "user-token",
		AnonKey:    "anon-key",
		ServiceKey: "service-key",
	}
	assert.Equal(t, "service-key", cfg.FunctionInvokeToken())

	cfg.ServiceKey = ""
	assert.Equal(t, "anon-key", cfg.FunctionInvokeToken())

	cfg.AnonKey = ""
	assert.Equal(t, "user-token", cfg.FunctionInvokeToken())
}

func TestWebURLFromEnv(t *testing.T) {
	t.Setenv("VOLCANO_WEB_URL", "http://localhost:3000")

	assert.Equal(t, "http://localhost:3000", Default().WebURL())
}

func TestWebURLDerivesFromAPIHostPrefix(t *testing.T) {
	t.Setenv(envWebURL, "")

	for _, tc := range []struct {
		apiURL string
		want   string
	}{
		{"https://api.volcano.dev", "https://volcano.dev"},
		{"https://api.staging.volcano.dev", "https://staging.volcano.dev"},
		{"http://api.example.com:8443", "http://example.com:8443"},
	} {
		t.Run(tc.apiURL, func(t *testing.T) {
			t.Setenv(envAPIURL, tc.apiURL)
			assert.Equal(t, tc.want, Default().WebURL())
		})
	}
}

func TestWebURLDerivesFromAPIHostPrefixIsCaseInsensitive(t *testing.T) {
	t.Setenv(envWebURL, "")
	t.Setenv(envAPIURL, "https://API.staging.volcano.dev")

	assert.Equal(t, "https://staging.volcano.dev", Default().WebURL())
}

func TestWebURLDerivesLocalhostForLoopbackAPIURL(t *testing.T) {
	t.Setenv(envWebURL, "")

	for _, apiURL := range []string{"http://localhost:8000", "http://127.0.0.1:8000", "http://[::1]:8000"} {
		t.Run(apiURL, func(t *testing.T) {
			t.Setenv(envAPIURL, apiURL)
			assert.Equal(t, "http://localhost:3000", Default().WebURL())
		})
	}
}

func TestWebURLExplicitCompiledDefaultWinsOverLoopbackDerivation(t *testing.T) {
	// `make local` bakes both VOLCANO_API_URL and VOLCANO_WEB_URL from .env.local
	// as compiled defaults. If the API URL is a loopback address but the developer
	// explicitly compiled in a non-conventional web URL (e.g. a frontend dev
	// server not on port 3000), that explicit choice must win over the :3000
	// loopback convention, not get silently overridden by it.
	t.Setenv(envWebURL, "")
	t.Setenv(envAPIURL, "http://localhost:8000")

	original := compiledDefaultWebURL
	compiledDefaultWebURL = "http://localhost:4000"
	t.Cleanup(func() { compiledDefaultWebURL = original })

	assert.Equal(t, "http://localhost:4000", Default().WebURL())
}

func TestWebURLFallsBackToCompiledDefaultWhenNoConventionMatches(t *testing.T) {
	t.Setenv(envWebURL, "")
	t.Setenv(envAPIURL, "https://backend.example.com")

	assert.Equal(t, defaultCompiledWebURL, Default().WebURL())
}

func TestWebURLEnvOverrideWinsOverDerivation(t *testing.T) {
	t.Setenv(envAPIURL, "https://api.staging.volcano.dev")
	t.Setenv(envWebURL, "http://localhost:3000")

	assert.Equal(t, "http://localhost:3000", Default().WebURL())
}

func TestIsLoopbackAPIURL(t *testing.T) {
	for _, tc := range []struct {
		url  string
		want bool
	}{
		{"http://localhost:8000", true},
		{"https://localhost", true},
		{"http://127.0.0.1:8000", true},
		{"http://[::1]:8000", true},
		{"https://api.volcano.dev", false},
		{"https://api.staging.volcano.dev", false},
		{"http://192.168.1.10:8000", false},
		{"", false},
		{"::not a url::", false},
	} {
		t.Run(tc.url, func(t *testing.T) {
			assert.Equal(t, tc.want, IsLoopbackAPIURL(tc.url))
		})
	}
}

func TestFunctionAliasesPersistByScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	scope := FunctionAliasScope("http://localhost:8000/", "project-123")
	otherScope := FunctionAliasScope("https://api.volcano.dev", "project-123")
	cfg := &Config{}
	cfg.SetFunctionAlias(scope, "hello", "33333333-3333-4333-8333-333333333333")
	cfg.SetFunctionAlias(otherScope, "hello", "44444444-4444-4444-8444-444444444444")
	require.NoError(t, cfg.Save())

	loaded, err := Load()
	require.NoError(t, err)

	got, ok := loaded.FunctionAlias(scope, "hello")
	require.True(t, ok)
	assert.Equal(t, "33333333-3333-4333-8333-333333333333", got)
	got, ok = loaded.FunctionAlias(otherScope, "hello")
	require.True(t, ok)
	assert.Equal(t, "44444444-4444-4444-8444-444444444444", got)
	assert.Equal(t, "http://localhost:8000|project-123", scope)
}

func TestDeleteFunctionAliasCleansEmptyScope(t *testing.T) {
	scope := FunctionAliasScope("https://api.volcano.dev", "project-123")
	cfg := &Config{}
	cfg.SetFunctionAlias(scope, "hello", "33333333-3333-4333-8333-333333333333")

	assert.True(t, cfg.DeleteFunctionAlias(scope, "hello"))
	assert.False(t, cfg.DeleteFunctionAlias(scope, "hello"))
	assert.Empty(t, cfg.FunctionAliases)
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

func TestCompiledDefaults(t *testing.T) {
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
	assert.Equal(t, "https://compiled.example", cfg.APIURL())

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
