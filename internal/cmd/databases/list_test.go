package databases

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cliconfig "github.com/Kong/volcano-cli/internal/config"
	cliruntime "github.com/Kong/volcano-cli/internal/runtime"
)

func TestDatabasesListEmptyOutput(t *testing.T) {
	setDatabaseCommandTestHome(t)
	saveDatabaseCommandTestConfig(t, &cliconfig.Config{
		UserToken: "token",
		CurrentProject: &cliconfig.ProjectConfig{
			ID:   databaseProjectID,
			Name: "Beta",
		},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeDatabaseCommandJSON(t, w, http.StatusOK, map[string]any{
			"data":     []any{},
			"has_more": false,
			"page":     1,
			"limit":    100,
			"total":    0,
		})
	}))
	defer server.Close()

	out, err := executeDatabaseCommand(t, New(cliruntime.Deps{HTTPClient: server.Client(), APIBaseURL: server.URL}), "list")
	require.NoError(t, err)
	assert.Contains(t, out, "No databases configured")
	assert.Contains(t, out, "Showing 0 of 0 database(s) (page 1, limit 100)")
}
