package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const sessionProjectID = "11111111-1111-4111-8111-111111111111"

func TestFactoryAuthenticatedRequiresAuth(t *testing.T) {
	setSessionTestHome(t)

	_, err := NewFactory(cliruntime.Deps{}).Authenticated()
	require.ErrorIs(t, err, config.ErrNotAuthenticated)
}

func TestFactoryCurrentProjectRequiresProject(t *testing.T) {
	setSessionTestHome(t)
	saveSessionTestConfig(t, &config.Config{UserToken: "token"})

	_, err := NewFactory(cliruntime.Deps{}).CurrentProject()
	require.ErrorIs(t, err, config.ErrNoProjectSelected)
}

func TestFactoryCurrentProjectReturnsCurrentProject(t *testing.T) {
	setSessionTestHome(t)
	saveSessionTestConfig(t, &config.Config{
		UserToken: "token",
		CurrentProject: &config.ProjectConfig{
			ID:   sessionProjectID,
			Name: "Alpha",
		},
	})

	authenticated, err := NewFactory(cliruntime.Deps{}).CurrentProject()
	require.NoError(t, err)
	require.NotNil(t, authenticated.Config)
	require.NotNil(t, authenticated.API)
	assert.Equal(t, sessionProjectID, authenticated.ProjectID.String())
}

func TestProjectSessionAPIWithTokenUsesSessionRuntimeDeps(t *testing.T) {
	setSessionTestHome(t)
	saveSessionTestConfig(t, &config.Config{
		UserToken: "user-token",
		CurrentProject: &config.ProjectConfig{
			ID:   sessionProjectID,
			Name: "Alpha",
		},
	})

	var sawAuth string
	var sawQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/projects", r.URL.Path)
		sawAuth = r.Header.Get("Authorization")
		sawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"data":[],"has_more":false,"page":2,"limit":3,"total":0}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	authenticated, err := NewFactory(cliruntime.Deps{
		APIBaseURL: server.URL,
		HTTPClient: server.Client(),
	}).CurrentProject()
	require.NoError(t, err)

	client, err := authenticated.APIWithToken("alternate-token")
	require.NoError(t, err)
	_, err = client.ListProjects(context.Background(), 2, 3)
	require.NoError(t, err)
	assert.Equal(t, "Bearer alternate-token", sawAuth)
	assert.Equal(t, "page=2&limit=3", sawQuery)
}

func TestFactoryCurrentProjectUsesEnvProject(t *testing.T) {
	setSessionTestHome(t)
	t.Setenv("VOLCANO_PROJECT_ID", sessionProjectID)
	saveSessionTestConfig(t, &config.Config{UserToken: "token"})

	authenticated, err := NewFactory(cliruntime.Deps{}).CurrentProject()
	require.NoError(t, err)
	assert.Equal(t, sessionProjectID, authenticated.ProjectID.String())
}

func TestFactoryCurrentProjectRejectsInvalidProject(t *testing.T) {
	setSessionTestHome(t)
	saveSessionTestConfig(t, &config.Config{
		UserToken: "token",
		CurrentProject: &config.ProjectConfig{
			ID:   "not-a-uuid",
			Name: "Broken",
		},
	})

	_, err := NewFactory(cliruntime.Deps{}).CurrentProject()
	require.ErrorContains(t, err, "invalid project ID")
}

func TestFactoryAPIURLAppliesDependencyOverride(t *testing.T) {
	setSessionTestHome(t)
	saveSessionTestConfig(t, &config.Config{UserToken: "token"})

	factory := NewFactory(cliruntime.Deps{APIBaseURL: "http://localhost:8000"})
	cfg, err := factory.Config()
	require.NoError(t, err)
	assert.Empty(t, cfg.APIBaseURL, "Config() must not mutate the loaded config")
	assert.Equal(t, "http://localhost:8000", factory.APIURL(cfg))
}

func setSessionTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveSessionTestConfig(t *testing.T, cfg *config.Config) {
	t.Helper()
	require.NoError(t, cfg.Save())
}
