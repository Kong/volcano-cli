package project

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Kong/volcano-cli/internal/api"
	"github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

// linkedToAlpha links a new directory to Alpha, writes cloudEnv to its
// volcano/cloud.env unless it is empty, and runs the test from a subdirectory.
func linkedToAlpha(t *testing.T, cloudEnv string) string {
	t.Helper()
	setProjectTestHome(t)
	saveProjectTestConfig(t, &config.Config{UserToken: "token"})
	dir := t.TempDir()
	_, err := config.WriteLink(dir, config.ProjectConfig{ID: projectAlphaID, Name: "Alpha"})
	require.NoError(t, err)
	if cloudEnv != "" {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "volcano"), 0o755))
		require.NoError(t, os.WriteFile(CloudEnvPath(dir), []byte(cloudEnv), 0o600))
	}
	sub := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(sub, 0o755))
	t.Chdir(sub)
	return dir
}

// betaServer answers Beta's project read and its anon keys with keys.
func betaServer(t *testing.T, keys http.HandlerFunc, anonKeyRequests *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects/" + projectBetaID:
			writeProjectJSON(t, w, http.StatusOK, projectTestPayload(projectBetaID, "Beta", "active"))
		case "/projects/" + projectBetaID + "/anon-keys":
			*anonKeyRequests++
			keys(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func betaAnonKeys(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, _ *http.Request) {
		writeProjectJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				map[string]any{"id": "33333333-3333-4333-8333-333333333333", "name": "old", "key_value": "ak-beta-old", "is_default": false},
				map[string]any{"id": "44444444-4444-4444-8444-444444444444", "name": "default", "key_value": "ak-beta", "is_default": true},
			},
		})
	}
}

func TestUseRewritesTheAnonKeyInTheLinkedCloudEnv(t *testing.T) {
	dir := linkedToAlpha(t, "# mine\nVOLCANO_API_URL=https://old.example\nVOLCANO_ANON_KEY=ak-alpha\nSTRIPE_KEY=sk\n"+
		"NEXT_PUBLIC_VOLCANO_ANON_KEY=ak-alpha\nexport VOLCANO_ANON_KEY=ak-alpha-dup\n")
	var anonKeyRequests int
	server := betaServer(t, betaAnonKeys(t), &anonKeyRequests)

	used, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), projectBetaID)
	require.NoError(t, err)

	require.NoError(t, used.EnvErr)
	assert.Equal(t, CloudEnvPath(dir), used.EnvFile)
	data, err := os.ReadFile(CloudEnvPath(dir))
	require.NoError(t, err)
	assert.Equal(t, "# mine\nVOLCANO_API_URL="+server.URL+"\nVOLCANO_ANON_KEY=ak-beta\nSTRIPE_KEY=sk\n"+
		"NEXT_PUBLIC_VOLCANO_ANON_KEY=ak-beta\nNEXT_PUBLIC_VOLCANO_API_URL="+server.URL+"\n", string(data))
	link, err := config.FindLink(dir)
	require.NoError(t, err)
	assert.Equal(t, config.ProjectConfig{ID: projectBetaID, Name: "Beta"}, link.Project)
}

func TestUseLeavesACloudEnvWithoutAnAnonKeyAlone(t *testing.T) {
	for name, cloudEnv := range map[string]string{
		"no file":        "",
		"no anon key":    "VOLCANO_API_URL=https://mine.example\nSTRIPE_KEY=sk\n",
		"commented only": "# VOLCANO_ANON_KEY=ak-alpha\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := linkedToAlpha(t, cloudEnv)
			var anonKeyRequests int
			server := betaServer(t, betaAnonKeys(t), &anonKeyRequests)

			used, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), projectBetaID)
			require.NoError(t, err)

			require.NoError(t, used.EnvErr)
			assert.Empty(t, used.EnvFile)
			assert.Zero(t, anonKeyRequests)
			data, err := os.ReadFile(CloudEnvPath(dir))
			if cloudEnv == "" {
				assert.ErrorIs(t, err, os.ErrNotExist, "use must not create the env file")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, cloudEnv, string(data))
		})
	}
}

func TestUseReportsACloudEnvItCouldNotRefresh(t *testing.T) {
	const cloudEnv = "VOLCANO_ANON_KEY=ak-alpha\n"
	dir := linkedToAlpha(t, cloudEnv)
	var anonKeyRequests int
	server := betaServer(t, func(w http.ResponseWriter, _ *http.Request) {
		writeProjectJSON(t, w, http.StatusInternalServerError, map[string]any{"error": "boom"})
	}, &anonKeyRequests)

	used, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), projectBetaID)
	require.NoError(t, err, "the link is saved even when the env file is not")

	require.ErrorContains(t, used.EnvErr, "failed to get the project's anon key")
	assert.Equal(t, CloudEnvPath(dir), used.EnvFile)
	data, err := os.ReadFile(CloudEnvPath(dir))
	require.NoError(t, err)
	assert.Equal(t, cloudEnv, string(data))
	link, err := config.FindLink(dir)
	require.NoError(t, err)
	assert.Equal(t, projectBetaID, link.Project.ID)
}

func TestUseRefusesToRelinkWhileVolcanoProjectIDNamesAnotherProject(t *testing.T) {
	for name, fromEnv := range map[string]string{"another project": projectAlphaID, "not an ID": "alpha"} {
		t.Run(name, func(t *testing.T) {
			const cloudEnv = "VOLCANO_ANON_KEY=ak-alpha\n"
			dir := linkedToAlpha(t, cloudEnv)
			t.Setenv("VOLCANO_PROJECT_ID", fromEnv)
			var anonKeyRequests int
			server := betaServer(t, betaAnonKeys(t), &anonKeyRequests)

			_, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), projectBetaID)
			require.ErrorContains(t, err, "VOLCANO_PROJECT_ID is set to "+fromEnv)
			assert.Zero(t, api.Status(err), "a refusal, not an API failure")
			require.NotErrorIs(t, err, ErrNotFound)

			assert.Zero(t, anonKeyRequests)
			data, err := os.ReadFile(CloudEnvPath(dir))
			require.NoError(t, err)
			assert.Equal(t, cloudEnv, string(data))
			link, err := config.FindLink(dir)
			require.NoError(t, err)
			assert.Equal(t, config.ProjectConfig{ID: projectAlphaID, Name: "Alpha"}, link.Project)
		})
	}
}

func TestUseRelinksWhenVolcanoProjectIDNamesTheSelectedProject(t *testing.T) {
	dir := linkedToAlpha(t, "VOLCANO_ANON_KEY=ak-alpha\n")
	t.Setenv("VOLCANO_PROJECT_ID", projectBetaID)
	var anonKeyRequests int
	server := betaServer(t, betaAnonKeys(t), &anonKeyRequests)

	used, err := NewService(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}).Use(context.Background(), projectBetaID)
	require.NoError(t, err)
	require.NoError(t, used.EnvErr)
	data, err := os.ReadFile(CloudEnvPath(dir))
	require.NoError(t, err)
	assert.Contains(t, string(data), "VOLCANO_ANON_KEY=ak-beta\n")
	link, err := config.FindLink(dir)
	require.NoError(t, err)
	assert.Equal(t, projectBetaID, link.Project.ID)
}

// cloud.env feeds NEXT_PUBLIC_VOLCANO_API_URL into browser bundles, so
// credentials or a query in the API URL must not reach it.
func TestWriteCloudEnvLeavesCredentialsOutOfTheAPIURL(t *testing.T) {
	dir := t.TempDir()

	require.NoError(t, WriteCloudEnv(dir, "https://user:secret@api.example.com/v1?token=abc#frag", "ak-1"))

	data, err := os.ReadFile(CloudEnvPath(dir))
	require.NoError(t, err)
	env := string(data)
	assert.Contains(t, env, "VOLCANO_API_URL=https://api.example.com/v1\n")
	assert.Contains(t, env, "NEXT_PUBLIC_VOLCANO_API_URL=https://api.example.com/v1\n")
	for _, leaked := range []string{"user", "secret", "token=abc", "frag"} {
		assert.NotContains(t, env, leaked)
	}
}
