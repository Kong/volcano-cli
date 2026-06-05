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

func TestDatabaseCommandsCreateListGetDelete(t *testing.T) {
	setDatabaseCommandTestHome(t)
	saveDatabaseCommandTestConfig(t, &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   databaseProjectID,
			Name: "Beta",
		},
	})

	var listQueries []string
	var createBodies []map[string]any
	var sawDelete bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/projects/"+databaseProjectID+"/databases":
			var createBody map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&createBody))
			createBodies = append(createBodies, createBody)
			writeDatabaseCommandJSON(t, w, http.StatusCreated, databaseCommandPayload("33333333-3333-4333-8333-333333333333", databaseProjectID, "app"))
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+databaseProjectID+"/databases":
			listQueries = append(listQueries, r.URL.RawQuery)
			switch r.URL.Query().Get("page") {
			case "1":
				writeDatabaseCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{databaseCommandPayload("33333333-3333-4333-8333-333333333333", databaseProjectID, "app")},
					"has_more": true,
					"page":     1,
					"limit":    100,
					"total":    2,
				})
			case "2":
				writeDatabaseCommandJSON(t, w, http.StatusOK, map[string]any{
					"data":     []any{databaseCommandPayload("44444444-4444-4444-8444-444444444444", databaseProjectID, "analytics")},
					"has_more": false,
					"page":     2,
					"limit":    25,
					"total":    2,
				})
			default:
				http.NotFound(w, r)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/projects/"+databaseProjectID+"/databases/app":
			writeDatabaseCommandJSON(t, w, http.StatusOK, databaseCommandPayload("33333333-3333-4333-8333-333333333333", databaseProjectID, "app"))
		case r.Method == http.MethodDelete && r.URL.Path == "/projects/"+databaseProjectID+"/databases/app":
			sawDelete = true
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	deps := cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}

	out, err := executeDatabaseCommand(t, New(deps), "create", "app", "--region", "aws-us-east-2", "--pg-version", "15", "--type", "volcano-db-s")
	require.NoError(t, err)
	require.Len(t, createBodies, 1)
	assert.Equal(t, map[string]any{
		"name":          "app",
		"region":        "aws-us-east-2",
		"pg_version":    "15",
		"database_type": "volcano-db-s",
	}, createBodies[0])
	assert.Contains(t, out, "Database 'app' created")
	assert.NotContains(t, out, "postgres://example")

	out, err = executeDatabaseCommand(t, New(deps), "create", "app", "--region", "aws-us-east-2", "--pg-version", "15", "--show-connection-string")
	require.NoError(t, err)
	assert.Contains(t, out, "postgres://example")

	out, err = executeDatabaseCommand(t, New(deps), "list")
	require.NoError(t, err)
	assert.Equal(t, []string{"page=1&limit=100"}, listQueries)
	assert.Contains(t, out, "app")
	assert.Contains(t, out, "Showing 1 of 2 database(s) (page 1, limit 100)")
	assert.Contains(t, out, "Next page: volcano databases list --page 2 --limit 100")
	assert.NotContains(t, out, "postgres://example")

	out, err = executeDatabaseCommand(t, New(deps), "list", "--page", "2", "--limit", "25", "--show-connection-string")
	require.NoError(t, err)
	assert.Equal(t, []string{"page=1&limit=100", "page=2&limit=25"}, listQueries)
	assert.Contains(t, out, "analytics")
	assert.Contains(t, out, "postgres://example")

	out, err = executeDatabaseCommand(t, New(deps), "get", "app")
	require.NoError(t, err)
	assert.Contains(t, out, "Name: app")
	assert.NotContains(t, out, "postgres://example")

	out, err = executeDatabaseCommand(t, New(deps), "get", "app", "--show-connection-string")
	require.NoError(t, err)
	assert.Contains(t, out, "postgres://example")

	out, err = executeDatabaseCommand(t, New(deps), "delete", "app", "--yes")
	require.NoError(t, err)
	assert.True(t, sawDelete)
	assert.Contains(t, out, "Database 'app' deleted")
}

func TestDatabaseCommandsRequireProject(t *testing.T) {
	setDatabaseCommandTestHome(t)
	saveDatabaseCommandTestConfig(t, &cliconfig.Config{UserToken: "token"})

	for _, args := range [][]string{
		{"list"},
		{"create", "app", "--region", "aws-us-east-1", "--pg-version", "16"},
		{"get", "app"},
		{"delete", "app", "--yes"},
	} {
		_, err := executeDatabaseCommand(t, New(cliruntime.Deps{}), args...)
		require.ErrorContains(t, err, "no project selected. Run 'volcano use <project-name>' or set VOLCANO_PROJECT_ID", "%v", args)
	}
}
