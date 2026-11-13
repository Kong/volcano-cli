package databases

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func catalogServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		switch r.URL.Path {
		case "/databases/regions":
			respondDatabaseJSON(t, w, http.StatusOK, []any{
				map[string]any{"id": "us-east-1", "name": "US East (N. Virginia)"},
				map[string]any{"id": "eu-west-1", "name": "Europe (Ireland)"},
			})
		case "/databases/postgres-versions":
			respondDatabaseJSON(t, w, http.StatusOK, []any{
				map[string]any{"version": "18", "name": "PostgreSQL 18", "default": true, "recommended": true},
				map[string]any{"version": "14", "name": "PostgreSQL 14", "deprecated": true},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// The catalog is public, so listing it needs neither a sign-in nor a project.
func TestDatabaseRegionsListsTheCatalogSignedOut(t *testing.T) {
	setDatabaseCommandTestHome(t)
	saveDatabaseCommandTestConfig(t, &cliconfig.Config{})
	server := catalogServer(t)
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeDatabaseCommand(t, New(deps), "regions")
	require.NoError(t, err)
	assert.Contains(t, out, "us-east-1")
	assert.Contains(t, out, "Europe (Ireland)")

	out, err = executeDatabaseCommand(t, New(deps), "regions", "--json")
	require.NoError(t, err)
	var regions []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &regions))
	assert.Equal(t, []map[string]any{
		{"id": "us-east-1", "name": "US East (N. Virginia)"},
		{"id": "eu-west-1", "name": "Europe (Ireland)"},
	}, regions)
}

func TestDatabaseVersionsMarksTheDefaultAndDeprecated(t *testing.T) {
	setDatabaseCommandTestHome(t)
	saveDatabaseCommandTestConfig(t, &cliconfig.Config{})
	server := catalogServer(t)
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeDatabaseCommand(t, New(deps), "versions")
	require.NoError(t, err)
	assert.Contains(t, out, "default, recommended")
	assert.Contains(t, out, "deprecated")

	out, err = executeDatabaseCommand(t, NewLocal(deps), "versions", "--json")
	require.NoError(t, err)
	var versions []map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &versions))
	require.Len(t, versions, 2)
	assert.Equal(t, map[string]any{
		"version": "14", "name": "PostgreSQL 14", "default": false, "recommended": false, "deprecated": true,
	}, versions[1])
}
