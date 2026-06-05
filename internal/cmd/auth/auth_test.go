package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

const authProjectID = "11111111-1111-4111-8111-111111111111"

func TestLoginTokenSuccessSavesConfig(t *testing.T) {
	setAuthTestHome(t)
	var sawAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects" {
			http.NotFound(w, r)
			return
		}
		sawAuth = r.Header.Get("Authorization")
		writeAuthJSON(t, w, http.StatusOK, map[string]any{
			"data": []any{
				map[string]any{
					"id": authProjectID,
				},
			},
			"has_more": false,
			"page":     1,
			"limit":    100,
			"total":    0,
		})
	}))
	defer server.Close()

	out, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "--token", "valid-token")
	require.NoError(t, err)
	assert.Equal(t, "Bearer valid-token", sawAuth)
	assert.Contains(t, out, "Token validated")
	assert.Contains(t, out, "Logged in successfully")

	cfg := loadAuthTestConfig(t)
	assert.Equal(t, "valid-token", cfg.UserToken)
	assert.Empty(t, cfg.UserID)
}

func TestLoginTokenInvalidFailsWithoutSavingConfig(t *testing.T) {
	setAuthTestHome(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeAuthJSON(t, w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	}))
	defer server.Close()

	_, err := executeAuthCommand(t, NewLogin(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "--token", "bad-token")
	require.ErrorContains(t, err, "token authentication failed: invalid token")

	path, err := cliconfig.Path()
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "config should not be saved after invalid token, stat err: %v", err)
}

func TestLogoutDeletesConfig(t *testing.T) {
	setAuthTestHome(t)
	saveAuthTestConfig(t, &cliconfig.Config{UserToken: "token"})

	out, err := executeAuthCommand(t, NewLogout())
	require.NoError(t, err)
	assert.Contains(t, out, "Logged out")

	path, err := cliconfig.Path()
	require.NoError(t, err)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "config exists after logout: %v", err)
}

func executeAuthCommand(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func setAuthTestHome(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLCANO_TOKEN", "")
	t.Setenv("VOLCANO_PROJECT_ID", "")
	t.Setenv("VOLCANO_API_URL", "")
	t.Setenv("VOLCANO_FIRST_PARTY_DEVICE_CLIENT_ID", "")
}

func saveAuthTestConfig(t *testing.T, cfg *cliconfig.Config) {
	t.Helper()
	require.NoError(t, cfg.Save())
}

func loadAuthTestConfig(t *testing.T) *cliconfig.Config {
	t.Helper()
	cfg, err := cliconfig.Load()
	require.NoError(t, err)
	return cfg
}

func writeAuthJSON(t *testing.T, w http.ResponseWriter, status int, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	require.NoError(t, json.NewEncoder(w).Encode(value))
}
